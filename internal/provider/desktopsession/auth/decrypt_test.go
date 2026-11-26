// Adapted from github.com/gammons/slk internal/slackdesktop/decrypt_test.go and cookie_test.go (MIT, Copyright (c) 2026 Grant Ammons).

package auth

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/sha1"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
)

// encryptCookie is Chromium's Linux cookie encryption: AES-128-CBC with a
// PBKDF2-SHA1 key (salt "saltysalt", one iteration), a 16-space IV and
// PKCS#7 padding, behind a version prefix.
func encryptCookie(t *testing.T, version string, plaintext []byte, password string) []byte {
	t.Helper()
	key, err := pbkdf2.Key(sha1.New, password, []byte("saltysalt"), 1, 16)
	if err != nil {
		t.Fatal(err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	pad := 16 - len(plaintext)%16
	padded := append(append([]byte{}, plaintext...), bytes.Repeat([]byte{byte(pad)}, pad)...)
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, bytes.Repeat([]byte{' '}, 16)).CryptBlocks(out, padded)
	return append([]byte(version), out...)
}

// passwords is a PasswordSource that returns fixed candidates and counts
// how often it was asked.
type passwords struct {
	keys  []string
	err   error
	calls int
}

func (p *passwords) Passwords(context.Context) ([][]byte, error) {
	p.calls++
	out := make([][]byte, len(p.keys))
	for i, k := range p.keys {
		out[i] = []byte(k)
	}
	return out, p.err
}

func TestDecryptV10UsesChromiumsFixedKey(t *testing.T) {
	src := &passwords{}
	got, err := decryptCookie(context.Background(), encryptCookie(t, "v10", []byte("xoxd-peanuts"), "peanuts"), src)
	if err != nil || got != "xoxd-peanuts" {
		t.Fatalf("decryptCookie = %q, %v", got, err)
	}
	if src.calls != 0 {
		t.Errorf("v10 asked the keyring %d times; its key is fixed", src.calls)
	}
}

// A store can hold a stale key next to the right one; every candidate is
// tried, from one lookup.
func TestDecryptV11TriesEveryKeyringCandidate(t *testing.T) {
	src := &passwords{keys: []string{"stale-key", "right-key"}}
	got, err := decryptCookie(context.Background(), encryptCookie(t, "v11", []byte("xoxd-keyring"), "right-key"), src)
	if err != nil || got != "xoxd-keyring" {
		t.Fatalf("decryptCookie = %q, %v", got, err)
	}
	if src.calls != 1 {
		t.Errorf("keyring asked %d times, want 1", src.calls)
	}
}

// Chromium 130 and later put SHA-256 of the cookie's host before the value.
func TestDecryptStripsTheDomainHash(t *testing.T) {
	for _, host := range []string{"slack.com", ".slack.com"} {
		sum := sha256.Sum256([]byte(host))
		plain := append(sum[:], []byte("xoxd-hashed")...)
		got, err := decryptCookie(context.Background(), encryptCookie(t, "v10", plain, "peanuts"), &passwords{})
		if err != nil || got != "xoxd-hashed" {
			t.Errorf("host %s: decryptCookie = %q, %v", host, got, err)
		}
	}
}

// wrongKeyPassingPadding returns a key that is not right yet whose CBC
// decryption still ends in valid PKCS#7 padding, as about one wrong key in
// sixteen does. Sent on, such a cookie made Slack answer invalid_auth, far
// from the real cause.
func wrongKeyPassingPadding(t *testing.T, enc []byte) string {
	t.Helper()
	for i := range 512 {
		key := fmt.Sprintf("wrong-key-%d", i)
		block, _ := cbcBlock([]byte(key))
		out := make([]byte, len(enc)-3)
		cipher.NewCBCDecrypter(block, bytes.Repeat([]byte{' '}, 16)).CryptBlocks(out, enc[3:])
		if n := int(out[len(out)-1]); n >= 1 && n <= 16 {
			return key
		}
	}
	t.Fatal("no wrong key produced valid padding in 512 tries")
	return ""
}

func TestDecryptRejectsGarbage(t *testing.T) {
	ctx := context.Background()
	enc := encryptCookie(t, "v11", []byte("xoxd-real-cookie-value"), "real-key")
	for name, tt := range map[string]struct {
		enc []byte
		src *passwords
	}{
		"wrong key with valid padding": {enc, &passwords{keys: []string{wrongKeyPassingPadding(t, enc)}}},
		"no version":                   {[]byte("v1"), &passwords{}},
		"not whole blocks":             {append([]byte("v10"), 1, 2, 3), &passwords{}},
		"empty body":                   {[]byte("v10"), &passwords{}},
		"bad padding":                  {encryptCookie(t, "v10", []byte("x"), "not-peanuts"), &passwords{}},
		"empty value":                  {encryptCookie(t, "v10", nil, "peanuts"), &passwords{}},
		"unprintable value":            {encryptCookie(t, "v10", []byte("xoxd-\x7f"), "peanuts"), &passwords{}},
	} {
		got, err := decryptCookie(ctx, tt.enc, tt.src)
		if !errors.Is(err, ErrDecryptFailed) {
			t.Errorf("%s: decryptCookie = %q, %v; want ErrDecryptFailed", name, got, err)
		}
	}
}

func TestDecryptV11ReportsTheKeyringFailure(t *testing.T) {
	ctx := context.Background()
	enc := encryptCookie(t, "v11", []byte("xoxd-x"), "k")
	if _, err := decryptCookie(ctx, enc, &passwords{err: ErrKeyringLocked}); !errors.Is(err, ErrKeyringLocked) {
		t.Errorf("error = %v, want ErrKeyringLocked", err)
	}
	if _, err := decryptCookie(ctx, enc, &passwords{}); !errors.Is(err, ErrSecretNotFound) {
		t.Errorf("error = %v, want ErrSecretNotFound", err)
	}
}

// The decryption error is shown to the user; it must not carry the
// decrypted bytes, which might be most of a real cookie.
func TestDecryptErrorDoesNotEchoTheValue(t *testing.T) {
	enc := encryptCookie(t, "v10", []byte("xoxd-secret\x01"), "peanuts")
	_, err := decryptCookie(context.Background(), enc, &passwords{})
	if err == nil || bytes.Contains([]byte(err.Error()), []byte("secret")) {
		t.Errorf("error = %v", err)
	}
}
