package keyring

import (
	"context"
	"errors"
	"os"
	"testing"
)

// TestRealSecretService runs against the Secret Service of the session it
// runs in. It is off by default because it writes to the user's keyring:
// set OMASLACK_TEST_REAL_SECRET_SERVICE=1 to run it. It stores and then
// removes one item for the made-up team TOMASLACKTEST.
func TestRealSecretService(t *testing.T) {
	if os.Getenv("OMASLACK_TEST_REAL_SECRET_SERVICE") != "1" {
		t.Skip("set OMASLACK_TEST_REAL_SECRET_SERVICE=1 to write to the real keyring")
	}
	ctx := context.Background()
	s := openStore(t)
	if s.Backend() != SecretService {
		t.Fatalf("Backend() = %q: no Secret Service on this session", s.Backend())
	}
	id := team(t, "TOMASLACKTEST")
	creds := DesktopSession{Token: "xoxc-omaslack-test", Cookie: "xoxd-omaslack-test"}
	t.Cleanup(func() { _ = s.Delete(ctx, id) })

	mustSet(t, s, id, creds)
	assertGet(t, s, id, creds)
	if err := s.Delete(ctx, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete error = %v, want ErrNotFound", err)
	}
}
