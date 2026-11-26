package internalapp

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
)

// The user token signs every step as a bearer token, the documented way:
// the Web API calls and the download. The pre-signed upload URL gets none.
func TestFilesTravelWithTheUserToken(t *testing.T) {
	var f *fakeSlack
	f = newFakeSlack(t, map[string]http.HandlerFunc{
		"files.getUploadURLExternal": func(w http.ResponseWriter, r *http.Request) {
			okJSON([]byte(`{"ok":true,"upload_url":"`+f.srv.URL+`/upload/v1/abc","file_id":"F0001"}`))(w, r)
		},
		"/upload/v1/abc":               func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "OK") },
		"files.completeUploadExternal": okJSON(readFixture(t, "files.completeUploadExternal.json")),
		"/files-pri/T0001-F0001/download/notes.txt": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "hello")
		},
	})
	p, _ := newTestProvider(t, f)
	var _ provider.FileUploader = p
	var _ provider.FileDownloader = p

	files, err := p.UploadFiles(context.Background(), mustConv(t, "C0001"), domain.TS{},
		[]provider.Upload{{Name: "notes.txt", Size: 5, Content: strings.NewReader("hello")}}, "notes")
	if err != nil || len(files) != 1 || files[0].ID != mustParse(t, domain.ParseFileID, "F0001") {
		t.Fatalf("UploadFiles() = %+v, %v", files, err)
	}
	file := files[0]
	file.DownloadURL = f.srv.URL + "/files-pri/T0001-F0001/download/notes.txt"
	var out bytes.Buffer
	if _, err := p.DownloadFile(context.Background(), file, &out, 100); err != nil || out.String() != "hello" {
		t.Fatalf("DownloadFile() = %q, %v", out.String(), err)
	}

	calls := f.recorded()
	if len(calls) != 4 {
		t.Fatalf("calls = %+v", calls)
	}
	for i, want := range []string{"Bearer " + testUserToken, "", "Bearer " + testUserToken, "Bearer " + testUserToken} {
		if calls[i].Auth != want {
			t.Errorf("%s: Authorization %q, want %q", calls[i].Method, calls[i].Auth, want)
		}
	}
	if calls[2].Form.Get("initial_comment") != "notes" || calls[2].Form.Get("channel_id") != "C0001" {
		t.Errorf("completion form = %v", calls[2].Form)
	}
}
