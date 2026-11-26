// Adapted from github.com/gammons/slk internal/slackdesktop/cookiedb.go, cookie.go and localstorage.go (MIT, Copyright (c) 2026 Grant Ammons).

package auth

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // the desktop app's Cookies database
)

const cookieQuery = `SELECT value, encrypted_value FROM cookies WHERE host_key = '.slack.com' AND name = 'd'`

// readCookie returns the desktop app's "d" cookie from its Cookies
// database: the plaintext value when Chromium stored one, otherwise the
// encrypted value decrypted with a key from src.
func readCookie(ctx context.Context, path string, src PasswordSource) (string, error) {
	var (
		plain string
		enc   []byte
	)
	err := withCopies(filepath.Dir(path), []string{filepath.Base(path)}, func(dir string) error {
		db, err := sql.Open("sqlite", filepath.Join(dir, filepath.Base(path)))
		if err != nil {
			return err
		}
		defer db.Close()
		return db.QueryRowContext(ctx, cookieQuery).Scan(&plain, &enc)
	})
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, sql.ErrNoRows):
		// No database, or one without the cookie: installed, never signed in.
		return "", ErrCookieDBMissing
	case err != nil:
		return "", err
	case plain != "":
		return plain, nil
	default:
		return decryptCookie(ctx, enc, src)
	}
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
