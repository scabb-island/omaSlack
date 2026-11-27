// Spike: read a Slack web session (xoxc token + d cookie) from a Chromium
// browser profile (Brave/Chrome/Chromium/Edge) and validate it against
// Slack's auth.test, the same way the desktop-session provider reuses the
// Slack desktop app's session. Throwaway: proves browsers store the
// session in the identical Chromium layout so the real feature can reuse
// internal/provider/desktopsession/auth.
//
// Run: go run ./spikes/browser-session [profileDir]
// Default profileDir: Brave's Default profile.
package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/godbus/dbus/v5"
	"github.com/syndtr/goleveldb/leveldb"
	_ "modernc.org/sqlite"

	"gitlab.com/scabb-island/omaslack/internal/keyring"
	"gitlab.com/scabb-island/omaslack/internal/keyring/secretservice"
	"gitlab.com/scabb-island/omaslack/internal/provider/desktopsession"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
}

func run() error {
	home := os.Getenv("HOME")
	profile := filepath.Join(home, ".config", "BraveSoftware", "Brave-Browser", "Default")
	label := "brave" // the keyring attribute is lowercase (brave, chromium, chrome)
	if len(os.Args) > 1 {
		profile = os.Args[1]
	}
	fmt.Println("Profile:", profile)

	// 1. xoxc tokens from Local Storage leveldb.
	tokens, err := readTokens(filepath.Join(profile, "Local Storage", "leveldb"))
	if err != nil {
		return fmt.Errorf("read tokens: %w", err)
	}
	fmt.Printf("Found %d workspace token(s) in localConfig_v2\n", len(tokens))
	for team := range tokens {
		fmt.Println("  team:", team)
	}
	if len(tokens) == 0 {
		return fmt.Errorf("no xoxc tokens: is this browser signed in to app.slack.com?")
	}

	// 2. Safe Storage key from the keyring.
	keys, err := safeStorageKeys(context.Background(), label)
	if err != nil {
		return fmt.Errorf("keyring: %w", err)
	}
	fmt.Printf("Got %d Safe Storage key candidate(s) from the keyring\n", len(keys))

	// 3. d cookie (and the whole .slack.com set) from the Cookies db.
	cookies, err := readCookies(cookiesPath(profile), keys)
	if err != nil {
		return fmt.Errorf("read cookies: %w", err)
	}
	if cookies.d == "" {
		return fmt.Errorf("no d cookie decrypted")
	}
	fmt.Printf("Decrypted d cookie (%d other .slack.com cookies)\n", len(cookies.others))

	// 4. Validate each session against Slack.
	for team, token := range tokens {
		creds := keyring.DesktopSession{
			Token:   token,
			Cookie:  encodeCookie(cookies.d),
			Cookies: cookies.header(),
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		id, err := desktopsession.AuthTest(desktopsession.DefaultAPIURL)(ctx, creds)
		cancel()
		if err != nil {
			fmt.Printf("  %s: REJECTED by Slack: %v\n", team, err)
			continue
		}
		fmt.Printf("  %s: OK -> team=%q url=%s\n", team, id.Team, id.URL)
	}
	return nil
}

func cookiesPath(profile string) string {
	net := filepath.Join(profile, "Network", "Cookies")
	if _, err := os.Stat(net); err == nil {
		return net
	}
	return filepath.Join(profile, "Cookies")
}

// --- tokens (leveldb) ---

func readTokens(dir string) (map[string]string, error) {
	tmp, cleanup, err := copyDir(dir)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	db, err := leveldb.OpenFile(tmp, nil)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	tokens := map[string]string{}
	iter := db.NewIterator(nil, nil)
	defer iter.Release()
	for iter.Next() {
		if !strings.HasSuffix(string(iter.Key()), "localConfig_v2") {
			continue
		}
		var cfg struct {
			Teams map[string]struct {
				Token string `json:"token"`
			} `json:"teams"`
		}
		if json.Unmarshal([]byte(decodeLSValue(iter.Value())), &cfg) != nil {
			continue
		}
		for id, t := range cfg.Teams {
			if t.Token != "" {
				tokens[id] = t.Token
			}
		}
	}
	return tokens, iter.Error()
}

func decodeLSValue(v []byte) string {
	if len(v) == 0 {
		return ""
	}
	switch v[0] {
	case 0x00:
		u16 := make([]uint16, (len(v)-1)/2)
		for i := range u16 {
			u16[i] = uint16(v[1+2*i]) | uint16(v[2+2*i])<<8
		}
		return string(utf16.Decode(u16))
	case 0x01:
		return string(v[1:])
	default:
		return string(v)
	}
}

// --- cookies (sqlite + decrypt) ---

type jar struct {
	d      string
	others [][2]string // name, value
}

func (j jar) header() string {
	parts := []string{"d=" + encodeCookie(j.d)}
	for _, c := range j.others {
		parts = append(parts, c[0]+"="+c[1])
	}
	return strings.Join(parts, "; ")
}

func readCookies(path string, keys [][]byte) (jar, error) {
	tmp, cleanup, err := copyDir(filepath.Dir(path))
	if err != nil {
		return jar{}, err
	}
	defer cleanup()
	db, err := sql.Open("sqlite", filepath.Join(tmp, filepath.Base(path)))
	if err != nil {
		return jar{}, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT host_key, name, value, encrypted_value FROM cookies
		WHERE host_key = 'slack.com' OR host_key LIKE '%.slack.com' ORDER BY host_key, name`)
	if err != nil {
		return jar{}, err
	}
	defer rows.Close()
	var j jar
	for rows.Next() {
		var host, name, plain string
		var enc []byte
		if err := rows.Scan(&host, &name, &plain, &enc); err != nil {
			return jar{}, err
		}
		value := plain
		if value == "" && len(enc) > 0 {
			value, err = decrypt(enc, keys, host)
			if err != nil {
				continue // skip cookies we cannot decrypt
			}
		}
		if host == ".slack.com" && name == "d" {
			j.d = value
		} else if value != "" {
			j.others = append(j.others, [2]string{name, value})
		}
	}
	return j, rows.Err()
}

func decrypt(enc []byte, keys [][]byte, host string) (string, error) {
	if body, ok := bytes.CutPrefix(enc, []byte("v10")); ok {
		return decryptWith(body, []byte("peanuts"), host)
	}
	body, ok := bytes.CutPrefix(enc, []byte("v11"))
	if !ok {
		return "", fmt.Errorf("unknown version")
	}
	var lastErr error
	for _, k := range keys {
		if v, err := decryptWith(body, k, host); err == nil {
			return v, nil
		} else {
			lastErr = err
		}
	}
	return "", lastErr
}

func decryptWith(body, password []byte, host string) (string, error) {
	if len(body) == 0 || len(body)%aes.BlockSize != 0 {
		return "", fmt.Errorf("bad length")
	}
	mac := hmac.New(sha1.New, password)
	mac.Write([]byte("saltysalt\x00\x00\x00\x01"))
	block, err := aes.NewCipher(mac.Sum(nil)[:16])
	if err != nil {
		return "", err
	}
	out := make([]byte, len(body))
	cipher.NewCBCDecrypter(block, bytes.Repeat([]byte{' '}, aes.BlockSize)).CryptBlocks(out, body)
	n := int(out[len(out)-1])
	if n > aes.BlockSize {
		return "", fmt.Errorf("bad padding")
	}
	out = out[:len(out)-n]
	bare := strings.TrimPrefix(host, ".")
	for _, h := range [][32]byte{sha256.Sum256([]byte("." + bare)), sha256.Sum256([]byte(bare))} {
		if rest, ok := bytes.CutPrefix(out, h[:]); ok {
			out = rest
			break
		}
	}
	if len(out) == 0 {
		return "", fmt.Errorf("empty")
	}
	for _, c := range out {
		if c < 0x20 || c > 0x7e {
			return "", fmt.Errorf("not printable")
		}
	}
	return string(out), nil
}

func encodeCookie(c string) string {
	if !strings.ContainsAny(c, "/+=") {
		return c
	}
	// minimal URL-escape of the three chars Slack encodes
	r := strings.NewReplacer("%", "%25", "/", "%2F", "+", "%2B", "=", "%3D")
	return r.Replace(c)
}

// --- keyring ---

func safeStorageKeys(ctx context.Context, label string) ([][]byte, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	s, err := secretservice.Open(ctx, conn)
	if err != nil {
		return nil, err
	}
	defer s.Close(ctx)
	var out [][]byte
	queries := []map[string]string{
		{"xdg:schema": "chrome_libsecret_os_crypt_password_v2", "application": label},
		{"xdg:schema": "chrome_libsecret_os_crypt_password_v1", "application": label},
		{"application": label},
	}
	for _, q := range queries {
		unlocked, _, err := s.Search(ctx, q)
		if err != nil {
			continue
		}
		for _, item := range unlocked {
			if secret, err := s.Secret(ctx, item); err == nil && len(secret) > 0 {
				out = append(out, secret)
			}
		}
	}
	// Always include the v10 fallback key.
	out = append(out, []byte("peanuts"))
	return out, nil
}

// --- temp copy (profile dbs are locked while the browser runs) ---

func copyDir(dir string) (string, func(), error) {
	tmp, err := os.MkdirTemp("", "omaslack-spike-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { os.RemoveAll(tmp) }
	entries, err := os.ReadDir(dir)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		if err := copyFile(filepath.Join(dir, e.Name()), filepath.Join(tmp, e.Name())); err != nil {
			cleanup()
			return "", nil, err
		}
	}
	return tmp, cleanup, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
