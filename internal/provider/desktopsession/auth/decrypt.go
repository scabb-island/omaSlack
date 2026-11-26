// Adapted from github.com/gammons/slk internal/slackdesktop/decrypt.go and cookie.go (MIT, Copyright (c) 2026 Grant Ammons).

package auth

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // Chromium derives the cookie key with PBKDF2-SHA1
	"crypto/sha256"
	"fmt"
	"strings"
)

// PasswordSource yields every candidate for the desktop app's "Slack Safe
// Storage" key, the password its v11 cookies are encrypted with. A store
// can hold a stale key next to the right one, so decryption tries each.
// It returns at least one candidate or an error.
type PasswordSource interface {
	Passwords(ctx context.Context) ([][]byte, error)
}

// v10Password is the fixed key Chromium on Linux uses when it has no
// keyring: v10 cookies need no lookup.
const v10Password = "peanuts"

// decryptCookie decrypts one .slack.com cookie value; see decrypter.
func decryptCookie(ctx context.Context, enc []byte, src PasswordSource) (string, error) {
	return newDecrypter(src).decrypt(ctx, enc, dCookieHost)
}

// decrypter decrypts the Chromium cookie values of one profile on Linux:
// "v10" with the fixed key, "v11" with a key from the keyring, which it
// asks src for once, at the first v11 value. Each candidate must yield
// plausible cookie text; padding alone lets a wrong key through about one
// time in sixteen.
type decrypter struct {
	src        PasswordSource
	asked      bool
	candidates [][]byte
	err        error
}

func newDecrypter(src PasswordSource) *decrypter { return &decrypter{src: src} }

// value is c's value: its plaintext when Chromium stored one, otherwise
// its encrypted value decrypted.
func (d *decrypter) value(ctx context.Context, c storedCookie) (string, error) {
	if c.plain != "" {
		return c.plain, nil
	}
	return d.decrypt(ctx, c.enc, c.host)
}

// decrypt decrypts enc, a value stored for host.
func (d *decrypter) decrypt(ctx context.Context, enc []byte, host string) (string, error) {
	if body, ok := bytes.CutPrefix(enc, []byte("v10")); ok {
		return decryptWith(body, []byte(v10Password), host)
	}
	body, ok := bytes.CutPrefix(enc, []byte("v11"))
	if !ok {
		return "", fmt.Errorf("%w: unknown encryption version", ErrDecryptFailed)
	}
	if !d.asked {
		d.asked = true
		d.candidates, d.err = d.src.Passwords(ctx)
	}
	if d.err != nil {
		return "", d.err
	}
	return firstDecryption(body, d.candidates, host)
}

func firstDecryption(body []byte, candidates [][]byte, host string) (string, error) {
	err := ErrSecretNotFound
	for _, password := range candidates {
		var cookie string
		if cookie, err = decryptWith(body, password, host); err == nil {
			return cookie, nil
		}
	}
	return "", err
}

// decryptWith decrypts body, a value stored for host, with a PBKDF2-SHA1
// key (salt "saltysalt", one iteration, 16 bytes) and a 16-space IV, then
// checks the result.
func decryptWith(body, password []byte, host string) (string, error) {
	if len(body) == 0 || len(body)%aes.BlockSize != 0 {
		return "", fmt.Errorf("%w: bad ciphertext length %d", ErrDecryptFailed, len(body))
	}
	block, err := aes.NewCipher(cookieKey(password))
	if err != nil {
		return "", err
	}
	out := make([]byte, len(body))
	cipher.NewCBCDecrypter(block, bytes.Repeat([]byte{' '}, aes.BlockSize)).CryptBlocks(out, body)
	// A zero pad length strips nothing and leaves a NUL, which checked
	// rejects as unprintable, so only the upper bound needs a check.
	n := int(out[len(out)-1])
	if n > aes.BlockSize {
		return "", fmt.Errorf("%w: bad padding", ErrDecryptFailed)
	}
	return checked(removeDomainHash(out[:len(out)-n], host))
}

// cookieKey is Chromium's AES-128 key: PBKDF2-HMAC-SHA1 of password with
// salt "saltysalt", one iteration, 16 bytes. With one iteration and a key
// shorter than the hash, PBKDF2 is a single HMAC of the salt followed by
// the block index 1 (RFC 8018, section 5.2). Computing it directly avoids
// crypto/pbkdf2, which FIPS-only mode refuses for a 9-byte salt.
func cookieKey(password []byte) []byte {
	mac := hmac.New(sha1.New, password)
	mac.Write([]byte("saltysalt\x00\x00\x00\x01"))
	return mac.Sum(nil)[:16]
}

// removeDomainHash strips the SHA-256 of the cookie's host that Chromium
// 130 and later put before the value. Both spellings of the host, with
// and without its leading dot, are seen.
func removeDomainHash(value []byte, host string) []byte {
	bare := strings.TrimPrefix(host, ".")
	for _, h := range [][32]byte{sha256.Sum256([]byte("." + bare)), sha256.Sum256([]byte(bare))} {
		if rest, ok := bytes.CutPrefix(value, h[:]); ok {
			return rest
		}
	}
	return value
}

// checked accepts only non-empty printable ASCII. It does not require the
// "xoxd-" prefix, which would break the day Slack renames the cookie. The
// error never quotes the value.
func checked(value []byte) (string, error) {
	if len(value) == 0 {
		return "", fmt.Errorf("%w: empty value", ErrDecryptFailed)
	}
	for _, c := range value {
		if c < 0x20 || c > 0x7e {
			return "", fmt.Errorf("%w: decrypted value is not printable text (wrong Safe Storage key?)", ErrDecryptFailed)
		}
	}
	return string(value), nil
}
