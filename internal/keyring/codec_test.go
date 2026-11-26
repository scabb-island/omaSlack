package keyring

import (
	"errors"
	"strings"
	"testing"
)

// Both backends store credentials as the same JSON document, which records
// the provider kind so it comes back as the right type.
func TestCodecRoundTripsEveryKind(t *testing.T) {
	for _, want := range []Credentials{
		DesktopSession{Token: "xoxc-1", Cookie: "xoxd-1"},
		InternalApp{UserToken: "xoxp-1", AppToken: "xapp-1"},
	} {
		data, err := encode(want)
		if err != nil {
			t.Fatalf("encode(%v): %v", want, err)
		}
		got, err := decode(data)
		if err != nil {
			t.Fatalf("decode(%s): %v", want, err)
		}
		if got != want {
			t.Errorf("round trip of %v gave %v", want, got)
		}
	}
}

func TestEncodeRecordsTheProviderKind(t *testing.T) {
	data, err := encode(DesktopSession{Token: "xoxc-1", Cookie: "xoxd-1"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"provider":"desktop-session","token":"xoxc-1","cookie":"xoxd-1"}`
	if string(data) != want {
		t.Errorf("encode = %s, want %s", data, want)
	}
}

func TestEncodeRejectsNoCredentials(t *testing.T) {
	if _, err := encode(nil); !errors.Is(err, ErrMalformed) {
		t.Errorf("encode(nil) error = %v, want ErrMalformed", err)
	}
}

func TestDecodeRejectsWhatItCannotParse(t *testing.T) {
	for _, data := range []string{
		`not json`,
		`{"provider":"carrier-pigeon","token":"x"}`,
		`{}`,
	} {
		if _, err := decode([]byte(data)); !errors.Is(err, ErrMalformed) {
			t.Errorf("decode(%s) error = %v, want ErrMalformed", data, err)
		}
	}
}

// A decode error ends up in logs; it must not echo the document, which
// holds the secrets.
func TestDecodeErrorDoesNotEchoSecrets(t *testing.T) {
	for _, data := range []string{
		`{"provider":"carrier-pigeon","token":"xoxc-secret"}`,
		`{"provider":"desktop-session","token":"xoxc-secret"`,
	} {
		_, err := decode([]byte(data))
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Errorf("decode error = %v", err)
		}
	}
}
