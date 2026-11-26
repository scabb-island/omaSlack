// Adapted from github.com/gammons/slk internal/slackdesktop/password_linux_test.go and kwallet_linux_test.go (MIT, Copyright (c) 2026 Grant Ammons).

package auth

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/godbus/dbus/v5"

	"gitlab.com/scabb-island/omaslack/internal/dbustest"
)

func found(keys ...string) attempt {
	return func(context.Context) ([][]byte, error) {
		out := make([][]byte, len(keys))
		for i, k := range keys {
			out[i] = []byte(k)
		}
		return out, nil
	}
}

func keysAsStrings(keys [][]byte) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = string(k)
	}
	return out
}

func TestGatherKeepsEveryKeyFound(t *testing.T) {
	boom := errors.New("boom")
	got, err := gather(context.Background(), []attempt{found("a"), failed(boom), found("b", "c")})
	if err != nil || !reflect.DeepEqual(keysAsStrings(got), []string{"a", "b", "c"}) {
		t.Errorf("gather = %q, %v", got, err)
	}
}

// With no key, the failure reported is the one the user can act on best:
// unlock the keyring, then an unexpected error, then a keyring that lacks
// the key, then no keyring at all.
func TestGatherReportsTheMostActionableFailure(t *testing.T) {
	boom := errors.New("boom")
	for _, tt := range []struct {
		name     string
		attempts []attempt
		want     error
	}{
		{"nothing to try", nil, ErrSecretNotFound},
		{"no keyring", []attempt{failed(ErrNoKeyring)}, ErrNoKeyring},
		{"not found beats no keyring", []attempt{failed(ErrNoKeyring), failed(ErrSecretNotFound)}, ErrSecretNotFound},
		{"unexpected beats not found", []attempt{failed(ErrSecretNotFound), failed(boom), failed(ErrNoKeyring)}, boom},
		{"locked beats everything", []attempt{failed(boom), failed(ErrKeyringLocked), failed(ErrSecretNotFound)}, ErrKeyringLocked},
	} {
		if _, err := gather(context.Background(), tt.attempts); !errors.Is(err, tt.want) {
			t.Errorf("%s: error = %v, want %v", tt.name, err, tt.want)
		}
	}
}

// fakeItems scripts a Secret Service: what each query (by xdg:schema, or
// server for QtKeychain) matches and what each item holds.
type fakeItems struct {
	unlocked  map[string][]dbus.ObjectPath
	locked    map[string][]dbus.ObjectPath
	searchErr error
	secrets   map[dbus.ObjectPath]string
	secretErr map[dbus.ObjectPath]error
	queries   []map[string]string
}

func (f *fakeItems) Search(_ context.Context, attrs map[string]string) (unlocked, locked []dbus.ObjectPath, err error) {
	f.queries = append(f.queries, attrs)
	key := attrs["xdg:schema"]
	return f.unlocked[key], f.locked[key], f.searchErr
}

func (f *fakeItems) Secret(_ context.Context, item dbus.ObjectPath) ([]byte, error) {
	return []byte(f.secrets[item]), f.secretErr[item]
}

const (
	schemaV2 = "chrome_libsecret_os_crypt_password_v2"
	schemaV1 = "chrome_libsecret_os_crypt_password_v1"
	schemaQt = "org.qt.keychain"
)

func TestSecretServiceKeysAsksEverySchemaChromiumUses(t *testing.T) {
	f := &fakeItems{
		unlocked: map[string][]dbus.ObjectPath{schemaV2: {"/v2"}, schemaQt: {"/qt", "/qt-empty"}},
		secrets:  map[dbus.ObjectPath]string{"/v2": "key-v2", "/qt": "key-qt"},
	}
	got, err := secretServiceKeys(context.Background(), f)
	if err != nil || !reflect.DeepEqual(keysAsStrings(got), []string{"key-v2", "key-qt"}) {
		t.Fatalf("keys = %q, %v", got, err)
	}
	want := []map[string]string{
		{"xdg:schema": schemaV2, "application": "Slack"},
		{"xdg:schema": schemaV1, "application": "Slack"},
		{"xdg:schema": schemaQt, "server": "Slack Keys", "user": "Slack Safe Storage"},
	}
	if !reflect.DeepEqual(f.queries, want) {
		t.Errorf("queries = %v\nwant      %v", f.queries, want)
	}
}

func TestSecretServiceKeysFailures(t *testing.T) {
	boom := errors.New("boom")
	for _, tt := range []struct {
		name string
		f    *fakeItems
		want error
	}{
		{"no match", &fakeItems{}, ErrSecretNotFound},
		{"locked match", &fakeItems{locked: map[string][]dbus.ObjectPath{schemaV1: {"/l"}}}, ErrKeyringLocked},
		{"empty secret", &fakeItems{unlocked: map[string][]dbus.ObjectPath{schemaV2: {"/e"}}}, ErrSecretNotFound},
		{"search fails", &fakeItems{searchErr: boom}, boom},
		{"read fails", &fakeItems{
			unlocked:  map[string][]dbus.ObjectPath{schemaV2: {"/b"}},
			secretErr: map[dbus.ObjectPath]error{"/b": boom},
		}, boom},
	} {
		if _, err := secretServiceKeys(context.Background(), tt.f); !errors.Is(err, tt.want) {
			t.Errorf("%s: error = %v, want %v", tt.name, err, tt.want)
		}
	}
}

// fakeKWallet scripts one kwalletd and records its calls.
type fakeKWallet struct {
	enabled  bool
	wallet   string
	isOpen   bool
	handle   int32
	hasEntry bool
	password string
	fail     map[string]error
	calls    []string
}

func workingKWallet() *fakeKWallet {
	return &fakeKWallet{enabled: true, wallet: "kdewallet", isOpen: true, handle: 7, hasEntry: true, password: "kwallet-key", fail: map[string]error{}}
}

func (f *fakeKWallet) record(call string) error {
	f.calls = append(f.calls, call)
	return f.fail[call]
}

func (f *fakeKWallet) IsEnabled(context.Context) (bool, error) {
	return f.enabled, f.record("isEnabled")
}

func (f *fakeKWallet) NetworkWallet(context.Context) (string, error) {
	return f.wallet, f.record("networkWallet")
}

func (f *fakeKWallet) IsOpen(_ context.Context, wallet string) (bool, error) {
	return f.isOpen, f.record("isOpen " + wallet)
}

func (f *fakeKWallet) Open(_ context.Context, wallet, appID string) (int32, error) {
	return f.handle, f.record("open " + wallet + " " + appID)
}

func (f *fakeKWallet) HasEntry(_ context.Context, handle int32, folder, key, appID string) (bool, error) {
	return f.hasEntry, f.record("hasEntry " + folder + "/" + key + " " + appID)
}

func (f *fakeKWallet) ReadPassword(_ context.Context, handle int32, folder, key, appID string) (string, error) {
	return f.password, f.record("readPassword " + folder + "/" + key + " " + appID)
}

func (f *fakeKWallet) Close(_ context.Context, handle int32, appID string) error {
	return f.record("close " + appID)
}

func TestKWalletKeyReadsSlacksEntryWithoutPrompting(t *testing.T) {
	f := workingKWallet()
	f.fail["close omaslack"] = errors.New("close failed")
	got, err := kwalletKey(context.Background(), f)
	if err != nil || !reflect.DeepEqual(keysAsStrings(got), []string{"kwallet-key"}) {
		t.Fatalf("kwalletKey = %q, %v", got, err)
	}
	want := []string{
		"isEnabled", "networkWallet", "isOpen kdewallet", "open kdewallet omaslack",
		"hasEntry Slack Keys/Slack Safe Storage omaslack",
		"readPassword Slack Keys/Slack Safe Storage omaslack",
		"close omaslack",
	}
	if !reflect.DeepEqual(f.calls, want) {
		t.Errorf("calls = %q\nwant    %q", f.calls, want)
	}
}

func TestKWalletKeyFailures(t *testing.T) {
	transport := errors.New("dbus: connection closed")
	for _, tt := range []struct {
		name      string
		mutate    func(*fakeKWallet)
		want      error
		notCalled string
	}{
		{"disabled", func(f *fakeKWallet) { f.enabled = false }, ErrSecretNotFound, "networkWallet"},
		{"no wallet", func(f *fakeKWallet) { f.wallet = "" }, ErrSecretNotFound, "isOpen "},
		{"closed wallet would prompt", func(f *fakeKWallet) { f.isOpen = false }, ErrKeyringLocked, "open kdewallet omaslack"},
		{"access denied", func(f *fakeKWallet) { f.handle = -1 }, ErrKeyringLocked, "hasEntry Slack Keys/Slack Safe Storage omaslack"},
		{"no entry", func(f *fakeKWallet) { f.hasEntry = false }, ErrSecretNotFound, "readPassword Slack Keys/Slack Safe Storage omaslack"},
		{"empty entry", func(f *fakeKWallet) { f.password = "" }, ErrSecretNotFound, ""},
		{"isEnabled fails", func(f *fakeKWallet) { f.fail["isEnabled"] = transport }, transport, "networkWallet"},
		{"networkWallet fails", func(f *fakeKWallet) { f.fail["networkWallet"] = transport }, transport, "isOpen "},
		{"isOpen fails", func(f *fakeKWallet) { f.fail["isOpen kdewallet"] = transport }, transport, "open kdewallet omaslack"},
		{"open fails", func(f *fakeKWallet) { f.fail["open kdewallet omaslack"] = transport }, transport, "hasEntry Slack Keys/Slack Safe Storage omaslack"},
		{"hasEntry fails", func(f *fakeKWallet) { f.fail["hasEntry Slack Keys/Slack Safe Storage omaslack"] = transport }, transport, ""},
		{"readPassword fails", func(f *fakeKWallet) { f.fail["readPassword Slack Keys/Slack Safe Storage omaslack"] = transport }, transport, ""},
	} {
		f := workingKWallet()
		tt.mutate(f)
		_, err := kwalletKey(context.Background(), f)
		if !errors.Is(err, tt.want) {
			t.Errorf("%s: error = %v, want %v", tt.name, err, tt.want)
		}
		if tt.notCalled != "" && slices.ContainsFunc(f.calls, func(c string) bool { return len(c) >= len(tt.notCalled) && c[:len(tt.notCalled)] == tt.notCalled }) {
			t.Errorf("%s: %q was called; calls = %q", tt.name, tt.notCalled, f.calls)
		}
		opened := slices.Contains(f.calls, "open kdewallet omaslack") && f.handle >= 0 && f.fail["open kdewallet omaslack"] == nil
		if closed := slices.Contains(f.calls, "close omaslack"); closed != opened {
			t.Errorf("%s: handle opened %t but closed %t", tt.name, opened, closed)
		}
	}
}

// kwalletServer serves fakeKWallet's answers on a test bus through the
// org.kde.KWallet method names.
func serveKWallet(t *testing.T, addr, name string, path dbus.ObjectPath, f *fakeKWallet) {
	t.Helper()
	conn := dbustest.Serve(t, addr, name)
	ctx := context.Background()
	methods := map[string]any{
		"isEnabled":     func() (bool, *dbus.Error) { v, _ := f.IsEnabled(ctx); return v, nil },
		"networkWallet": func() (string, *dbus.Error) { v, _ := f.NetworkWallet(ctx); return v, nil },
		"isOpen":        func(w string) (bool, *dbus.Error) { v, _ := f.IsOpen(ctx, w); return v, nil },
		"open": func(w string, _ int64, app string) (int32, *dbus.Error) {
			v, _ := f.Open(ctx, w, app)
			return v, nil
		},
		"hasEntry": func(h int32, folder, key, app string) (bool, *dbus.Error) {
			v, _ := f.HasEntry(ctx, h, folder, key, app)
			return v, nil
		},
		"readPassword": func(h int32, folder, key, app string) (string, *dbus.Error) {
			v, _ := f.ReadPassword(ctx, h, folder, key, app)
			return v, nil
		},
		"close": func(h int32, force bool, app string) (int32, *dbus.Error) {
			_ = f.Close(ctx, h, app)
			return 0, nil
		},
	}
	if err := conn.ExportMethodTable(methods, path, "org.kde.KWallet"); err != nil {
		t.Fatal(err)
	}
}

func TestSystemPasswordsReadsTheSecretService(t *testing.T) {
	addr := dbustest.StartBus(t)
	svc := &dbustest.SecretService{}
	svc.Add(map[string]string{"xdg:schema": schemaV2, "application": "Slack"}, []byte("libsecret-key"))
	dbustest.ServeSecretService(t, addr, svc)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", addr)

	got, err := SystemPasswords{}.Passwords(context.Background())
	if err != nil || !reflect.DeepEqual(keysAsStrings(got), []string{"libsecret-key"}) {
		t.Fatalf("Passwords = %q, %v", got, err)
	}
	if calls := svc.Calls(); calls[len(calls)-1] != "Session.Close" {
		t.Errorf("session left open: calls = %q", calls)
	}
}

func TestSystemPasswordsReadsKWallet(t *testing.T) {
	addr := dbustest.StartBus(t)
	f := workingKWallet()
	serveKWallet(t, addr, "org.kde.kwalletd6", "/modules/kwalletd6", f)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", addr)

	got, err := SystemPasswords{}.Passwords(context.Background())
	if err != nil || !reflect.DeepEqual(keysAsStrings(got), []string{"kwallet-key"}) {
		t.Fatalf("Passwords = %q, %v", got, err)
	}
	if !slices.Contains(f.calls, "close omaslack") {
		t.Errorf("KWallet handle left open: calls = %q", f.calls)
	}
}

func TestSystemPasswordsWithoutAKeyring(t *testing.T) {
	for name, addr := range map[string]func(*testing.T) string{
		"empty bus": dbustest.StartBus,
		"no bus":    func(t *testing.T) string { return "unix:path=" + filepath.Join(t.TempDir(), "no-bus") },
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("DBUS_SESSION_BUS_ADDRESS", addr(t))
			if _, err := (SystemPasswords{}).Passwords(context.Background()); !errors.Is(err, ErrNoKeyring) {
				t.Errorf("error = %v, want ErrNoKeyring", err)
			}
		})
	}
}

// A Secret Service that is there but refuses a session is an unexpected
// failure, not "no keyring".
func TestSystemPasswordsKeepsAProviderRefusal(t *testing.T) {
	addr := dbustest.StartBus(t)
	dbustest.ServeSecretService(t, addr, &dbustest.SecretService{
		OpenErr: dbus.NewError("org.freedesktop.DBus.Error.NotSupported", nil),
	})
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", addr)
	_, err := SystemPasswords{}.Passwords(context.Background())
	if err == nil || errors.Is(err, ErrNoKeyring) {
		t.Errorf("error = %v, want the provider's refusal", err)
	}
}
