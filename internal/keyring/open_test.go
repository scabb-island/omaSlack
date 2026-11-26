package keyring

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/godbus/dbus/v5"

	"gitlab.com/scabb-island/omaslack/internal/dbustest"
)

// isolate points the session bus and the config directory at test-owned
// places, so Open can never reach the user's keyring or ~/.config.
func isolate(t *testing.T, busAddr string) (configHome string) {
	t.Helper()
	configHome = t.TempDir()
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", busAddr)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("HOME", t.TempDir())
	return configHome
}

func openStore(t *testing.T) ClosableStore {
	t.Helper()
	s, err := Open(context.Background())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestOpenPrefersTheSecretService(t *testing.T) {
	addr := dbustest.StartBus(t)
	svc := &dbustest.SecretService{}
	dbustest.ServeSecretService(t, addr, svc)
	configHome := isolate(t, addr)

	s := openStore(t)
	if s.Backend() != SecretService {
		t.Fatalf("Backend() = %q, want %q", s.Backend(), SecretService)
	}
	creds := DesktopSession{Token: "xoxc-1", Cookie: "xoxd-1"}
	mustSet(t, s, team(t, "T1A"), creds)
	assertGet(t, s, team(t, "T1A"), creds)

	want := []map[string]string{{"service": "omaslack", "team": "T1A"}}
	if items := svc.Items(); !reflect.DeepEqual(items, want) {
		t.Errorf("Secret Service items = %v, want %v", items, want)
	}
	if _, err := os.Stat(filepath.Join(configHome, "omaslack")); !os.IsNotExist(err) {
		t.Errorf("Open with a Secret Service touched the config dir: %v", err)
	}
}

func TestOpenFallsBackToTheFile(t *testing.T) {
	for name, addr := range map[string]func(t *testing.T) string{
		"no provider on the bus": dbustest.StartBus,
		"no bus at all": func(t *testing.T) string {
			return "unix:path=" + filepath.Join(t.TempDir(), "no-bus")
		},
	} {
		t.Run(name, func(t *testing.T) {
			configHome := isolate(t, addr(t))
			s := openStore(t)
			if s.Backend() != File {
				t.Fatalf("Backend() = %q, want %q", s.Backend(), File)
			}
			mustSet(t, s, team(t, "T1A"), DesktopSession{Token: "xoxc-1"})
			if _, err := os.Stat(filepath.Join(configHome, "omaslack", "credentials")); err != nil {
				t.Errorf("credentials file: %v", err)
			}
		})
	}
}

// A provider that is there but broken is not "no Secret Service": falling
// back would hide credentials already stored in it.
func TestOpenReportsABrokenProvider(t *testing.T) {
	addr := dbustest.StartBus(t)
	dbustest.ServeSecretService(t, addr, &dbustest.SecretService{
		OpenErr: dbus.NewError("org.freedesktop.DBus.Error.NotSupported", nil),
	})
	isolate(t, addr)
	if s, err := Open(context.Background()); err == nil {
		t.Errorf("Open() = %s store, want an error", s.Backend())
	}
}

func TestOpenNeedsAConfigDirForTheFile(t *testing.T) {
	isolate(t, "unix:path="+filepath.Join(t.TempDir(), "no-bus"))
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	if s, err := Open(context.Background()); err == nil {
		t.Errorf("Open() = %s store, want an error", s.Backend())
	}
}
