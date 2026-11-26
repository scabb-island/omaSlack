package syncengine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
)

// Files (task 2.9): uploads of local files and downloads into the cache.
// Both go through the budget at Interactive priority and touch neither
// the Store nor the published events: an upload's message arrives on the
// stream like any other, with its files.
//
// The download cache is <FileCache>/<team>/<file ID>/<name>. Directories
// are 0700 and files 0600; a download is written to a temporary file in
// the same directory and renamed into place, so a cached path always
// holds a whole file, and a second fetch returns it without asking Slack.
// The name is Slack's, made safe as one path component (safeName).

// The budget keys of files: the Slack call behind each action.
const (
	methodUpload   = "files.completeUploadExternal"
	methodDownload = "files.download"
)

// Default limits; Config.MaxDownload and Config.MaxUpload replace them.
const (
	DefaultMaxDownload int64 = 100 << 20 // 100 MiB
	DefaultMaxUpload   int64 = 1 << 30   // 1 GiB, Slack's own limit
)

// ErrNotRegularFile reports an upload path that is not a regular file (a
// directory, a device, a pipe).
var ErrNotRegularFile = errors.New("not a regular file")

// maxNameBytes bounds a cached file's name, well under the 255 bytes most
// file systems allow.
const maxNameBytes = 200

// defaultFileCache is $XDG_CACHE_HOME/omaslack/files, or
// ~/.cache/omaslack/files when it is unset; "" when neither is known.
func defaultFileCache() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "omaslack", "files")
}

// FetchFile returns the local path of a file a stored message carries,
// downloading it into the cache unless it is already there. The path is
// only valid on this machine.
func (e *Engine) FetchFile(ctx context.Context, id domain.FileID) (string, error) {
	d, err := optional[provider.FileDownloader](e, "fetch a file", domain.Files)
	if err != nil {
		return "", err
	}
	f, err := e.cfg.Store.File(ctx, id)
	if err != nil {
		return "", err
	}
	if e.cfg.FileCache == "" {
		return "", errors.New("syncengine: fetch a file: no cache directory (set $XDG_CACHE_HOME or $HOME)")
	}
	dir := filepath.Join(e.cfg.FileCache, e.w.team.String(), f.ID.String())
	path := filepath.Join(dir, safeName(f))
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
		return path, nil
	}
	if f.Size > e.cfg.MaxDownload {
		return "", fmt.Errorf("syncengine: fetch %s (%d bytes): %w", f.ID, f.Size, provider.ErrFileTooLarge)
	}
	return path, e.download(ctx, d, f, dir, path)
}

// download writes f to path through a temporary file in dir.
func (e *Engine) download(ctx context.Context, d provider.FileDownloader, f domain.File, dir, path string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("syncengine: fetch %s: %w", f.ID, err)
	}
	tmp, err := os.CreateTemp(dir, ".fetch-*")
	if err != nil {
		return fmt.Errorf("syncengine: fetch %s: %w", f.ID, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // fails once renamed, as it should
	err = e.interactive(ctx, methodDownload, func(ctx context.Context) error {
		if err := rewind(tmp); err != nil {
			return err
		}
		_, err := d.DownloadFile(ctx, f, tmp, e.cfg.MaxDownload)
		return err
	})
	err = errors.Join(err, tmp.Close())
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	return err
}

// rewind empties a temporary file, so a retried download starts over.
func rewind(f *os.File) error {
	if err := f.Truncate(0); err != nil {
		return err
	}
	_, err := f.Seek(0, io.SeekStart)
	return err
}

// safeName is f's name as one path component: path separators, NUL and
// other control characters become "_", invalid UTF-8 is replaced, and it
// is cut to maxNameBytes. A name left empty, "." or ".." is the file ID.
func safeName(f domain.File) string {
	name := strings.Map(pathSafe, strings.ToValidUTF8(f.Name, "_"))
	for len(name) > maxNameBytes {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	if name == "" || name == "." || name == ".." {
		return f.ID.String()
	}
	return name
}

// pathSafe maps a rune of a name to one that is safe in a path
// component: separators and control characters become "_".
func pathSafe(r rune) rune {
	if r == '/' || r == '\\' || r < 0x20 || r == 0x7f {
		return '_'
	}
	return r
}

// UploadFiles shares local files in conv (inside thread when it is not
// zero) as one message with caption, and returns the files as Slack
// stored them. Each path must be a regular file of at most MaxUpload
// bytes; the daemon reads it as the user it runs as. Every file is
// checked before Slack is asked anything.
func (e *Engine) UploadFiles(ctx context.Context, conv domain.ConversationID, thread domain.TS, paths []string, caption string) ([]domain.File, error) {
	up, err := optional[provider.FileUploader](e, "upload files", domain.Files)
	if err != nil {
		return nil, err
	}
	locals, err := openUploads(paths, e.cfg.MaxUpload)
	defer closeAll(locals)
	if err != nil {
		return nil, err
	}
	return ask(ctx, e, methodUpload, func(ctx context.Context) ([]domain.File, error) {
		uploads, err := fromStart(locals)
		if err != nil {
			return nil, err
		}
		return up.UploadFiles(ctx, conv, thread, uploads, caption)
	})
}

// localFile is an open upload and its size when it was checked.
type localFile struct {
	f    *os.File
	size int64
}

// openUploads opens and checks every path. On an error the files opened
// so far are returned too, for the caller to close.
func openUploads(paths []string, max int64) ([]localFile, error) {
	var out []localFile
	for _, p := range paths {
		l, err := openUpload(p, max)
		if err != nil {
			return out, err
		}
		out = append(out, l)
	}
	return out, nil
}

// openUpload opens path and checks the opened file, so what is checked is
// what is sent.
func openUpload(path string, max int64) (localFile, error) {
	f, err := os.Open(path) //nolint:gosec // the client's own file: it runs as the same user
	if err != nil {
		return localFile{}, fmt.Errorf("syncengine: upload: %w", err)
	}
	l := localFile{f: f}
	info, err := f.Stat()
	switch {
	case err != nil:
		err = fmt.Errorf("syncengine: upload %s: %w", path, err)
	case !info.Mode().IsRegular():
		err = fmt.Errorf("syncengine: upload %s: %w", path, ErrNotRegularFile)
	case info.Size() > max:
		err = fmt.Errorf("syncengine: upload %s (%d bytes): %w", path, info.Size(), provider.ErrFileTooLarge)
	}
	if err != nil {
		_ = f.Close()
		return localFile{}, err
	}
	l.size = info.Size()
	return l, nil
}

// fromStart rewinds every file, so a retried upload sends each whole.
func fromStart(locals []localFile) ([]provider.Upload, error) {
	out := make([]provider.Upload, 0, len(locals))
	for _, l := range locals {
		if _, err := l.f.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		out = append(out, provider.Upload{Name: filepath.Base(l.f.Name()), Size: l.size, Content: l.f})
	}
	return out, nil
}

func closeAll(locals []localFile) {
	for _, l := range locals {
		_ = l.f.Close()
	}
}
