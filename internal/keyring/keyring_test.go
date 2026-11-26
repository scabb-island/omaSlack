package keyring

import (
	"fmt"
	"strings"
	"testing"
)

// Credentials end up in logs and error messages by accident; printing them
// must never reveal a secret.
func TestCredentialsNeverPrintSecrets(t *testing.T) {
	creds := []Credentials{
		DesktopSession{
			Token: "xoxc-secret-token", Cookie: "xoxd-secret-cookie",
			Cookies: "d=xoxd-secret-cookie; x=secret-x; d-s=secret-ds",
		},
		InternalApp{UserToken: "xoxp-secret-user", AppToken: "xapp-secret-app"},
	}
	for _, c := range creds {
		for _, verb := range []string{"%v", "%+v", "%s", "%#v"} {
			out := fmt.Sprintf(verb, c)
			if strings.Contains(out, "secret") {
				t.Errorf("%T printed with %s reveals a secret: %s", c, verb, out)
			}
		}
	}
}

func TestCredentialsNameTheirProvider(t *testing.T) {
	if got := (DesktopSession{}).Provider(); got != "desktop-session" {
		t.Errorf("DesktopSession.Provider() = %q", got)
	}
	if got := (InternalApp{}).Provider(); got != "internal-app" {
		t.Errorf("InternalApp.Provider() = %q", got)
	}
}

func TestRedactedFormShowsWhatIsSet(t *testing.T) {
	got := fmt.Sprint(DesktopSession{Token: "xoxc-1", Cookie: ""})
	want := "desktop-session{token:set cookie:missing cookies:missing}"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	got = fmt.Sprint(InternalApp{UserToken: "", AppToken: "xapp-1"})
	want = "internal-app{userToken:missing appToken:set}"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// An entry stored before omaslack kept every session cookie sends the d
// cookie alone, as it always did; a newer one sends the whole set.
func TestCookieHeaderIsTheFullSetWhenKnown(t *testing.T) {
	old := DesktopSession{Token: "xoxc-1", Cookie: "xoxd-1"}
	if got := old.CookieHeader(); got != "d=xoxd-1" {
		t.Errorf("old entry CookieHeader() = %q, want d=xoxd-1", got)
	}
	full := DesktopSession{Token: "xoxc-1", Cookie: "xoxd-1", Cookies: "b=1; d=xoxd-1; x=2"}
	if got := full.CookieHeader(); got != "b=1; d=xoxd-1; x=2" {
		t.Errorf("CookieHeader() = %q, want every cookie", got)
	}
}
