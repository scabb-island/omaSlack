package keyring

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"testing"

	"github.com/godbus/dbus/v5"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/keyring/secretservice"
)

// fakeSecrets is an in-memory Secret Service behind the secrets interface.
type fakeSecrets struct {
	items  map[dbus.ObjectPath]fakeItem
	next   int
	locked bool
	fail   map[string]error // by method name
	open   int              // sessions opened and not yet closed
}

type fakeItem struct {
	attrs       map[string]string
	value       []byte
	label       string
	contentType string
}

func newFakeSecrets() *fakeSecrets {
	return &fakeSecrets{items: map[dbus.ObjectPath]fakeItem{}, fail: map[string]error{}}
}

func (f *fakeSecrets) store() *SecretServiceStore {
	return newSecretServiceStore(func(context.Context) (secrets, error) {
		if err := f.fail["Open"]; err != nil {
			return nil, err
		}
		f.open++
		return f, nil
	}, func() error { return nil })
}

func (f *fakeSecrets) add(attrs map[string]string, value string) dbus.ObjectPath {
	f.next++
	path := dbus.ObjectPath(fmt.Sprintf("/item/%d", f.next))
	f.items[path] = fakeItem{attrs: attrs, value: []byte(value)}
	return path
}

func (f *fakeSecrets) Search(_ context.Context, attrs map[string]string) (unlocked, locked []dbus.ObjectPath, err error) {
	for path, it := range f.items {
		if matches(it.attrs, attrs) {
			unlocked = append(unlocked, path)
		}
	}
	if f.locked {
		return nil, unlocked, f.fail["Search"]
	}
	return unlocked, nil, f.fail["Search"]
}

func matches(have, want map[string]string) bool {
	for k, v := range want {
		if have[k] != v {
			return false
		}
	}
	return true
}

func (f *fakeSecrets) Secret(_ context.Context, item dbus.ObjectPath) ([]byte, error) {
	return f.items[item].value, f.fail["Secret"]
}

func (f *fakeSecrets) Attributes(_ context.Context, item dbus.ObjectPath) (map[string]string, error) {
	return f.items[item].attrs, f.fail["Attributes"]
}

func (f *fakeSecrets) Create(_ context.Context, label string, attrs map[string]string, value []byte, contentType string) error {
	if err := f.fail["Create"]; err != nil {
		return err
	}
	for path, it := range f.items {
		if maps.Equal(it.attrs, attrs) {
			delete(f.items, path)
		}
	}
	path := f.add(attrs, string(value))
	f.items[path] = fakeItem{attrs: attrs, value: value, label: label, contentType: contentType}
	return nil
}

func (f *fakeSecrets) Delete(_ context.Context, item dbus.ObjectPath) error {
	if err := f.fail["Delete"]; err != nil {
		return err
	}
	delete(f.items, item)
	return nil
}

func (f *fakeSecrets) Close(context.Context) error {
	f.open--
	return nil
}

func TestSecretServiceStoreReportsItsBackend(t *testing.T) {
	if b := newFakeSecrets().store().Backend(); b != SecretService {
		t.Errorf("Backend() = %q, want %q", b, SecretService)
	}
}

func TestSecretServiceStoreKeepsOneItemPerTeam(t *testing.T) {
	ctx := context.Background()
	f := newFakeSecrets()
	s := f.store()
	app := InternalApp{UserToken: "xoxp-1", AppToken: "xapp-1"}
	mustSet(t, s, team(t, "T2B"), DesktopSession{Token: "xoxc-2"})
	mustSet(t, s, team(t, "T1A"), DesktopSession{Token: "old"})
	mustSet(t, s, team(t, "T1A"), app)

	assertGet(t, s, team(t, "T1A"), app)
	teams, err := s.Teams(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := []domain.TeamID{team(t, "T1A"), team(t, "T2B")}; !reflect.DeepEqual(teams, want) {
		t.Errorf("Teams() = %v, want %v", teams, want)
	}

	var stored []fakeItem
	for _, it := range f.items {
		if it.attrs["team"] == "T1A" {
			stored = append(stored, it)
		}
	}
	want := fakeItem{
		attrs:       map[string]string{"service": "omaslack", "team": "T1A"},
		value:       []byte(`{"provider":"internal-app","user_token":"xoxp-1","app_token":"xapp-1"}`),
		label:       "omaslack credentials for T1A",
		contentType: "application/json",
	}
	if len(stored) != 1 || !reflect.DeepEqual(stored[0], want) {
		t.Errorf("stored items for T1A = %+v, want just %+v", stored, want)
	}
	if f.open != 0 {
		t.Errorf("%d sessions left open", f.open)
	}
}

func TestSecretServiceStoreGetMissingIsNotFound(t *testing.T) {
	f := newFakeSecrets()
	f.add(map[string]string{"service": "other", "team": "T1A"}, "{}")
	if _, err := f.store().Get(context.Background(), team(t, "T1A")); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get error = %v, want ErrNotFound", err)
	}
}

func TestSecretServiceStoreGetLockedIsReported(t *testing.T) {
	f := newFakeSecrets()
	f.add(map[string]string{"service": "omaslack", "team": "T1A"}, "{}")
	f.locked = true
	if _, err := f.store().Get(context.Background(), team(t, "T1A")); !errors.Is(err, secretservice.ErrLocked) {
		t.Errorf("Get error = %v, want ErrLocked", err)
	}
}

func TestSecretServiceStoreRejectsMalformedItems(t *testing.T) {
	ctx := context.Background()
	f := newFakeSecrets()
	f.add(map[string]string{"service": "omaslack", "team": "T1A"}, "not json")
	if _, err := f.store().Get(ctx, team(t, "T1A")); !errors.Is(err, ErrMalformed) {
		t.Errorf("Get error = %v, want ErrMalformed", err)
	}
	f.add(map[string]string{"service": "omaslack", "team": "nope"}, "{}")
	if _, err := f.store().Teams(ctx); !errors.Is(err, ErrMalformed) {
		t.Errorf("Teams error = %v, want ErrMalformed", err)
	}
	if err := f.store().Set(ctx, team(t, "T1A"), nil); !errors.Is(err, ErrMalformed) {
		t.Errorf("Set(nil) error = %v, want ErrMalformed", err)
	}
}

func TestSecretServiceStoreDeleteIsIdempotent(t *testing.T) {
	ctx := context.Background()
	f := newFakeSecrets()
	s := f.store()
	f.add(map[string]string{"service": "omaslack", "team": "T1A"}, "{}")
	f.add(map[string]string{"service": "omaslack", "team": "T1A"}, "{}")
	f.add(map[string]string{"service": "omaslack", "team": "T2B"}, "{}")
	for range 2 {
		if err := s.Delete(ctx, team(t, "T1A")); err != nil {
			t.Fatalf("Delete: %v", err)
		}
	}
	teams, err := s.Teams(ctx)
	if err != nil || !reflect.DeepEqual(teams, []domain.TeamID{team(t, "T2B")}) {
		t.Errorf("Teams() after Delete = %v, %v", teams, err)
	}
}

func TestSecretServiceStoreReportsBusFailures(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("boom")
	for _, method := range []string{"Open", "Search", "Secret", "Attributes", "Create", "Delete"} {
		t.Run(method, func(t *testing.T) {
			f := newFakeSecrets()
			f.add(map[string]string{"service": "omaslack", "team": "T1A"}, `{"provider":"desktop-session"}`)
			f.fail[method] = boom
			s := f.store()
			_, getErr := s.Get(ctx, team(t, "T1A"))
			_, teamsErr := s.Teams(ctx)
			errs := map[string]error{
				"Get":    getErr,
				"Teams":  teamsErr,
				"Set":    s.Set(ctx, team(t, "T1A"), DesktopSession{}),
				"Delete": s.Delete(ctx, team(t, "T1A")),
			}
			failed := 0
			for _, err := range errs {
				if errors.Is(err, boom) {
					failed++
				}
			}
			if failed == 0 {
				t.Errorf("no operation reported the %s failure: %v", method, errs)
			}
			if f.open != 0 {
				t.Errorf("%d sessions left open", f.open)
			}
		})
	}
}
