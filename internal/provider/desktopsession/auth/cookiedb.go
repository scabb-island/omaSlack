// Adapted from github.com/gammons/slk internal/slackdesktop/cookiedb.go, cookie.go and localstorage.go (MIT, Copyright (c) 2026 Grant Ammons).

package auth

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite" // the desktop app's Cookies database
)

// cookieQuery reads every cookie of Slack's domains, in a stable order.
// Which of them a workspace gets is decided by appliesTo.
const cookieQuery = `SELECT host_key, name, value, encrypted_value FROM cookies
	WHERE host_key = 'slack.com' OR host_key LIKE '%.slack.com' ORDER BY host_key, name`

// dCookieHost is where the desktop app keeps the d cookie.
const dCookieHost = ".slack.com"

// storedCookie is one cookie row of the desktop app, still encrypted when
// Chromium stored no plaintext value.
type storedCookie struct {
	host, name, plain string
	enc               []byte
}

// jar is the desktop app's Slack cookies, decrypted: the d cookie and
// every other one with the host it belongs to.
type jar struct {
	d      string
	others []storedCookie // plain holds the decrypted value
}

// readCookies reads the desktop app's Slack cookies from its Cookies
// database and decrypts them with keys from src, asked for once. The d
// cookie must be there and decrypt; any other that does not decrypt is
// skipped and named in log, never with its value.
func readCookies(ctx context.Context, path string, src PasswordSource, log *slog.Logger) (jar, error) {
	rows, err := queryCookies(ctx, path)
	if err != nil {
		return jar{}, err
	}
	dec := newDecrypter(src)
	var j jar
	for _, c := range rows {
		value, err := dec.value(ctx, c)
		switch {
		case c.host == dCookieHost && c.name == "d":
			if err != nil {
				return jar{}, err
			}
			j.d = value
		case err != nil:
			log.Warn("Slack session: skipped a cookie that cannot be decrypted", "name", c.name, "host", c.host)
		default:
			c.plain = value
			j.others = append(j.others, c)
		}
	}
	if j.d == "" {
		// A database without the d cookie: installed, never signed in.
		return jar{}, ErrCookieDBMissing
	}
	return j, nil
}

// queryCookies returns the Slack rows of the Cookies database at path,
// read from a temporary copy.
func queryCookies(ctx context.Context, path string) ([]storedCookie, error) {
	var out []storedCookie
	err := withCopies(filepath.Dir(path), []string{filepath.Base(path)}, func(dir string) error {
		db, err := sql.Open("sqlite", filepath.Join(dir, filepath.Base(path)))
		if err != nil {
			return err
		}
		defer db.Close()
		rows, err := db.QueryContext(ctx, cookieQuery)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c storedCookie
			if err := rows.Scan(&c.host, &c.name, &c.plain, &c.enc); err != nil {
				return err
			}
			out = append(out, c)
		}
		return rows.Err()
	})
	if errors.Is(err, fs.ErrNotExist) {
		// No database: installed, never signed in.
		return nil, ErrCookieDBMissing
	}
	return out, err
}

// header is the Cookie header for the workspace at host: the d cookie, then
// every other cookie a browser would send there, in the database's order.
func (j jar) header(host string) string {
	cookies := make([]cookie, 0, len(j.others)+1)
	cookies = append(cookies, cookie{"d", j.d})
	for _, c := range j.others {
		if appliesTo(c.host, host) && (cookie{c.name, c.plain}).valid() {
			cookies = append(cookies, cookie{c.name, c.plain})
		}
	}
	return cookieHeader(cookies, j.d)
}

// appliesTo reports whether a cookie stored for hostKey goes with a
// request to host: a domain cookie (".slack.com") to the domain and every
// host under it, a host-only one to its host alone.
func appliesTo(hostKey, host string) bool {
	if domain, ok := strings.CutPrefix(hostKey, "."); ok {
		return host == domain || strings.HasSuffix(host, hostKey)
	}
	return host == hostKey
}

// withCopies copies the named files of dir into a fresh temporary
// directory, runs fn on that directory and removes it. Slack keeps its
// databases open while it runs; working on copies neither locks them nor
// leaves side files (-wal, -shm, LOCK) in its profile.
func withCopies(dir string, names []string, fn func(copyDir string) error) error {
	tmp, err := os.MkdirTemp("", "omaslack-slack-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	for _, name := range names {
		if err := copyFile(filepath.Join(dir, name), filepath.Join(tmp, name)); err != nil {
			return err
		}
	}
	return fn(tmp)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec // the desktop app's own files
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // dst is in our own temp dir
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	return errors.Join(copyErr, out.Close())
}
