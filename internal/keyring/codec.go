package keyring

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ErrMalformed reports stored credentials that cannot be read back, or
// credentials of no known kind. Its messages never include the stored
// document, which holds the secrets.
var ErrMalformed = errors.New("malformed credentials")

// record is the JSON document both backends store for one workspace. The
// provider field says which Credentials kind the other fields belong to.
type record struct {
	Provider  string `json:"provider"`
	Token     string `json:"token,omitempty"`
	Cookie    string `json:"cookie,omitempty"`
	UserToken string `json:"user_token,omitempty"`
	AppToken  string `json:"app_token,omitempty"`
}

func encode(creds Credentials) ([]byte, error) {
	r, err := toRecord(creds)
	if err != nil {
		return nil, err
	}
	return json.Marshal(r)
}

func toRecord(creds Credentials) (record, error) {
	switch c := creds.(type) {
	case DesktopSession:
		return record{Provider: c.Provider(), Token: c.Token, Cookie: c.Cookie}, nil
	case InternalApp:
		return record{Provider: c.Provider(), UserToken: c.UserToken, AppToken: c.AppToken}, nil
	default:
		return record{}, fmt.Errorf("%w: unsupported kind %T", ErrMalformed, creds)
	}
}

// decode parses a stored document once, where it is read back. The JSON
// error is dropped on purpose: its text can quote the document.
func decode(data []byte) (Credentials, error) {
	var r record
	if json.Unmarshal(data, &r) != nil {
		return nil, fmt.Errorf("%w: not a JSON document", ErrMalformed)
	}
	return r.credentials()
}

func (r record) credentials() (Credentials, error) {
	switch r.Provider {
	case DesktopSession{}.Provider():
		return DesktopSession{Token: r.Token, Cookie: r.Cookie}, nil
	case InternalApp{}.Provider():
		return InternalApp{UserToken: r.UserToken, AppToken: r.AppToken}, nil
	default:
		return nil, fmt.Errorf("%w: unknown provider %q", ErrMalformed, r.Provider)
	}
}
