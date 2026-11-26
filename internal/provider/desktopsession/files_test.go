package desktopsession

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

// The web client's credentials sign every step: the Web API calls carry
// the xoxc token in the form and the d cookie; the download carries the
// token as a bearer and the cookie.
func TestFilesTravelWithTheWebClientsCredentials(t *testing.T) {
	var f *fakeSlack
	f = newFakeSlack(t, map[string]http.HandlerFunc{
		"/api/files.getUploadURLExternal": func(w http.ResponseWriter, r *http.Request) {
			jsonBody(http.StatusOK, `{"ok":true,"upload_url":"`+f.srv.URL+`/upload/v1/abc","file_id":"F0001"}`)(w, r)
		},
		"/upload/v1/abc":                    func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "OK") },
		"/api/files.completeUploadExternal": jsonBody(http.StatusOK, string(fixture(t, "files.completeUploadExternal.json"))),
		"/files-pri/T0001-F0001/download/notes.txt": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "hello")
		},
	})
	p, _ := f.provider(t)
	var _ provider.FileUploader = p
	var _ provider.FileDownloader = p

	files, err := p.UploadFiles(context.Background(), parse(t, domain.ParseConversationID, "C0001"), domain.TS{},
		[]provider.Upload{{Name: "notes.txt", Size: 5, Content: strings.NewReader("hello")}}, "")
	if err != nil || len(files) != 1 {
		t.Fatalf("UploadFiles() = %+v, %v", files, err)
	}
	// The stored URL points at Slack; fetch the same path from the fake.
	file := files[0]
	file.DownloadURL = f.srv.URL + "/files-pri/T0001-F0001/download/notes.txt"
	var out bytes.Buffer
	if _, err := p.DownloadFile(context.Background(), file, &out, 100); err != nil || out.String() != "hello" {
		t.Fatalf("DownloadFile() = %q, %v", out.String(), err)
	}

	reqs := f.requests()
	for _, i := range []int{0, 2} {
		if reqs[i].Form.Get("token") != testToken || reqs[i].Cookie != testCookie {
			t.Errorf("%s: token %q cookie %q, want the web client's", reqs[i].Path, reqs[i].Form.Get("token"), reqs[i].Cookie)
		}
	}
	if reqs[1].Cookie != "" || reqs[1].Header.Get("Authorization") != "" {
		t.Errorf("the pre-signed upload URL got credentials: %+v", reqs[1])
	}
	if d := reqs[3]; d.Header.Get("Authorization") != "Bearer "+testToken || d.Cookie != testCookie {
		t.Errorf("download headers = %v, want the bearer token and the d cookie", d.Header)
	}
}
