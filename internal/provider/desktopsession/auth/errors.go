// Adapted from github.com/gammons/slk internal/slackdesktop/errors.go (MIT, Copyright (c) 2026 Grant Ammons).

package auth

import (
	"errors"

	"gitlab.com/scabb-island/omaslack/internal/keyring/secretservice"
)

var (
	ErrDesktopNotFound = errors.New("the Slack desktop app config directory was not found")
	ErrNotSignedIn     = errors.New("the Slack desktop app is not signed in to any workspace")
	ErrCookieDBMissing = errors.New("the Slack desktop app cookie database was not found")
	ErrDecryptFailed   = errors.New("failed to decrypt the Slack session cookie")

	// ErrKeyringLocked reports a keyring or wallet that holds the cookie key
	// but must be unlocked first.
	ErrKeyringLocked = secretservice.ErrLocked
	// ErrNoKeyring reports that neither a Secret Service nor KWallet is
	// running, so the cookie key cannot be read.
	ErrNoKeyring = errors.New("no Secret Service or KWallet is running")
	// ErrSecretNotFound reports a keyring that answered but holds no Slack
	// cookie key.
	ErrSecretNotFound = errors.New("the Slack cookie key is not in the keyring")

	ErrNoToken  = errors.New("no xoxc- token found: copy a request to /api/, not the page itself")
	ErrNoCookie = errors.New("no d cookie (xoxd-) found: copy the request from a browser signed in to app.slack.com")
)
