package syncengine_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
	"gitlab.com/scabb-island/omaslack/internal/store"
	"gitlab.com/scabb-island/omaslack/internal/syncengine"
)

var (
	_ provider.FileUploader   = (*writes)(nil)
	_ provider.FileDownloader = (*writes)(nil)
)

// fileLimit is the rig's upload and download limit, in bytes.
const fileLimit = 64

// UploadFiles reads every upload whole (a retry must send it whole
// again) and records the call with what it read.
func (w *writes) UploadFiles(_ context.Context, conv domain.ConversationID, thread domain.TS, files []provider.Upload, caption string) ([]domain.File, error) {
	var sent []string
	var out []domain.File
	for i, f := range files {
		b, err := io.ReadAll(f.Content)
		if err != nil {
			return nil, err
		}
		sent = append(sent, fmt.Sprintf("%s(%d)=%s", f.Name, f.Size, b))
		out = append(out, domain.File{ID: must(domain.ParseFileID(fmt.Sprintf("F%04d", i+1))), Name: f.Name, Size: f.Size})
	}
	return out, w.record("files.completeUploadExternal", conv, show(thread), sent, caption)
}

// DownloadFile writes the file's ID as its content, repeated to its
// declared size, and enforces max like a provider: at most max+1 bytes.
func (w *writes) DownloadFile(_ context.Context, f domain.File, out io.Writer, max int64) (int64, error) {
	if err := w.record("files.download", f.ID); err != nil {
		return 0, err
	}
	content, ok := w.served[f.ID]
	if !ok {
		content = contentOf(f)
	}
	if int64(len(content)) > max {
		n, _ := io.WriteString(out, content[:max+1])
		return int64(n), provider.ErrFileTooLarge
	}
	n, err := io.WriteString(out, content)
	return int64(n), err
}

// contentOf is a file's content by default: its ID, padded to its size.
func contentOf(f domain.File) string {
	return strings.Repeat("x", max(0, int(f.Size)-len(f.ID.String()))) + f.ID.String()
}

func show(t domain.TS) string {
	if t.IsZero() {
		return "-"
	}
	return t.String()
}

// sharedFile is a file a message in convA carries.
func sharedFile(id, name string, size int64) domain.File {
	return domain.File{ID: must(domain.ParseFileID(id)), Name: name, Size: size, DownloadURL: "https://files.slack.com/" + id}
}

// filesRig is a running rig with the files capability, its cache under a
// fresh $XDG_CACHE_HOME, and files in its store.
func filesRig(t *testing.T, files ...domain.File) (*rig, *writes, string) {
	t.Helper()
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	r, w := newWritesRig(t, domain.Files)
	m := msg(ts1)
	m.Files = files
	r.cache(t, convA, m)
	r.connected(t)
	return r, w, cache
}

func TestFetchFileDownloadsOnceIntoTheCache(t *testing.T) {
	f := sharedFile("F0001", "report.pdf", 20)
	r, w, cache := filesRig(t, f)

	path, err := r.engine.FetchFile(bg, f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(cache, "omaslack", "files", "T01", "F0001", "report.pdf"); path != want {
		t.Fatalf("path = %s, want %s", path, want)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != contentOf(f) {
		t.Fatalf("content = %q, %v", b, err)
	}
	r.expectGrant(t, "files.download")
	expectMode(t, path, 0o600)
	expectMode(t, filepath.Dir(path), 0o700|os.ModeDir)
	expectMode(t, filepath.Dir(filepath.Dir(path)), 0o700|os.ModeDir)

	again, err := r.engine.FetchFile(bg, f.ID)
	if err != nil || again != path {
		t.Fatalf("second fetch = %s, %v", again, err)
	}
	if calls := w.recorded(); len(calls) != 1 {
		t.Fatalf("downloads = %q, want one", calls)
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Fatalf("cache dir holds %d entries, want only the file", len(entries))
	}
}

func expectMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode() != want {
		t.Errorf("%s mode = %v, want %v", path, info.Mode(), want)
	}
}

// A name from Slack is outside data: whatever it holds, the file lands
// inside its own directory of the cache.
func TestFetchFileNeverEscapesTheCache(t *testing.T) {
	files := []domain.File{
		sharedFile("F0001", "../../../.bashrc", 10),
		sharedFile("F0002", "..", 10),
		sharedFile("F0003", "", 10),
		sharedFile("F0004", "a/b\x00c", 10),
	}
	r, _, cache := filesRig(t, files...)
	for _, f := range files {
		path, err := r.engine.FetchFile(bg, f.ID)
		if err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(cache, "omaslack", "files", "T01", f.ID.String())
		if filepath.Dir(path) != dir {
			t.Errorf("%q landed at %s, outside %s", f.Name, path, dir)
		}
	}
}

func TestFetchFileRefusesWhatIsTooLarge(t *testing.T) {
	declared := sharedFile("F0001", "big.bin", fileLimit+1)
	lying := sharedFile("F0002", "small.bin", fileLimit+1)
	r, w, cache := filesRig(t, declared, lying)

	if _, err := r.engine.FetchFile(bg, declared.ID); !errors.Is(err, provider.ErrFileTooLarge) {
		t.Fatalf("declared too large: err = %v", err)
	}
	if calls := w.recorded(); len(calls) != 0 {
		t.Fatalf("a file declared too large was downloaded: %q", calls)
	}

	// Slack said it was small; the bytes said otherwise.
	w.served = map[domain.FileID]string{lying.ID: strings.Repeat("x", fileLimit+1)}
	lying.Size = 1
	r.cache(t, convA, domain.Message{TS: ts2, Text: "small", Files: []domain.File{lying}})
	if _, err := r.engine.FetchFile(bg, lying.ID); !errors.Is(err, provider.ErrFileTooLarge) {
		t.Fatalf("streamed too large: err = %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(cache, "omaslack", "files", "T01", "F0002")); len(entries) != 0 {
		t.Fatalf("a refused download left %d entries in the cache", len(entries))
	}
}

func TestAFetchRetriedAfterARateLimitStartsOver(t *testing.T) {
	f := sharedFile("F0001", "a.txt", 10)
	r, w, _ := filesRig(t, f)
	w.failNext("files.download", &provider.RateLimitedError{Method: "files.download"})

	path, err := r.engine.FetchFile(bg, f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != contentOf(f) {
		t.Fatalf("content = %q, want it once", b)
	}
}

func TestFetchFileFailures(t *testing.T) {
	r, w, _ := filesRig(t, domain.File{ID: must(domain.ParseFileID("F0001")), Name: "doc"})
	if _, err := r.engine.FetchFile(bg, must(domain.ParseFileID("F0404"))); !errors.Is(err, store.ErrUnknownFile) {
		t.Errorf("unknown file: err = %v", err)
	}
	refused := errors.New("file_not_found")
	w.failNext("files.download", refused)
	if _, err := r.engine.FetchFile(bg, must(domain.ParseFileID("F0001"))); !errors.Is(err, refused) {
		t.Errorf("refused download: err = %v", err)
	}
}

func TestFilesNeedTheFilesCapability(t *testing.T) {
	without, _ := newWritesRig(t)
	bareRig := newBareRig(t)
	for name, r := range map[string]*rig{"no capability": without, "no interface": bareRig} {
		r.connected(t)
		if _, err := r.engine.FetchFile(bg, must(domain.ParseFileID("F0001"))); !errors.Is(err, domain.ErrUnsupported) {
			t.Errorf("%s: FetchFile err = %v", name, err)
		}
		if _, err := r.engine.UploadFiles(bg, convA, zeroTS, []string{"/etc/hostname"}, ""); !errors.Is(err, domain.ErrUnsupported) {
			t.Errorf("%s: UploadFiles err = %v", name, err)
		}
	}
}

func TestFilesNeedTheEngineRunning(t *testing.T) {
	r, _ := newWritesRig(t, domain.Files)
	if _, err := r.engine.FetchFile(bg, must(domain.ParseFileID("F0001"))); !errors.Is(err, syncengine.ErrStopped) {
		t.Errorf("FetchFile err = %v", err)
	}
	if _, err := r.engine.UploadFiles(bg, convA, zeroTS, nil, ""); !errors.Is(err, syncengine.ErrStopped) {
		t.Errorf("UploadFiles err = %v", err)
	}
}

// localFile writes content to a new file and returns its path.
func localFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUploadFilesSharesLocalFiles(t *testing.T) {
	r, w, _ := filesRig(t)
	paths := []string{localFile(t, "a.txt", "alpha"), localFile(t, "b.png", "beta")}

	got, err := r.engine.UploadFiles(bg, convA, ts1, paths, "look")
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.File{{ID: must(domain.ParseFileID("F0001")), Name: "a.txt", Size: 5}, {ID: must(domain.ParseFileID("F0002")), Name: "b.png", Size: 4}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UploadFiles = %+v, want %+v", got, want)
	}
	if calls := w.recorded(); !reflect.DeepEqual(calls, []string{"files.completeUploadExternal CAAA " + ts1.String() + " [a.txt(5)=alpha b.png(4)=beta] look\n"}) {
		t.Fatalf("calls = %q", calls)
	}
	r.expectGrant(t, "files.completeUploadExternal")
}

func TestAnUploadRetriedAfterARateLimitSendsTheFilesWhole(t *testing.T) {
	r, w, _ := filesRig(t)
	w.failNext("files.completeUploadExternal", &provider.RateLimitedError{Method: "files.completeUploadExternal"})
	if _, err := r.engine.UploadFiles(bg, convA, zeroTS, []string{localFile(t, "a.txt", "alpha")}, ""); err != nil {
		t.Fatal(err)
	}
	calls := w.recorded()
	if len(calls) != 2 || calls[0] != calls[1] {
		t.Fatalf("calls = %q, want the same upload twice", calls)
	}
}

func TestUploadFilesRefusesWhatItCannotSend(t *testing.T) {
	r, w, _ := filesRig(t)
	ok := localFile(t, "ok.txt", "fine")
	for name, tc := range map[string]struct {
		path string
		want error
	}{
		"missing":       {filepath.Join(t.TempDir(), "nope"), os.ErrNotExist},
		"a directory":   {t.TempDir(), syncengine.ErrNotRegularFile},
		"too large":     {localFile(t, "big", strings.Repeat("x", fileLimit+1)), provider.ErrFileTooLarge},
		"a device file": {os.DevNull, syncengine.ErrNotRegularFile},
	} {
		if _, err := r.engine.UploadFiles(bg, convA, zeroTS, []string{ok, tc.path}, ""); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", name, err, tc.want)
		}
	}
	if calls := w.recorded(); len(calls) != 0 {
		t.Fatalf("Slack was asked: %q", calls)
	}
}
