package slackwire

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
)

// fileRequest is one request the fake files host received.
type fileRequest struct {
	Path   string
	Form   url.Values // Web API calls only
	Body   string     // uploads only
	Header http.Header
}

// filesSlack is a fake Web API and files host on one server: the Web API
// under /api/, upload URLs under /upload/ and downloads under /files-pri/.
type filesSlack struct {
	t        *testing.T
	srv      *httptest.Server
	mu       sync.Mutex
	requests []fileRequest
	answers  map[string]string // Web API method → body
	download func(w http.ResponseWriter)
}

func newFilesSlack(t *testing.T, caps ...domain.Capability) (*WebAPI, *filesSlack) {
	t.Helper()
	f := &filesSlack{t: t, answers: map[string]string{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	sign := func(form url.Values, h http.Header) { form.Set("token", "xoxc-secret"); h.Set("Cookie", "d=cookie") }
	return NewWebAPI(f.srv.URL+"/api/", f.srv.Client(), sign, domain.NewCapabilities(caps...), slog.New(slog.DiscardHandler)), f
}

func (f *filesSlack) serve(w http.ResponseWriter, r *http.Request) {
	req := fileRequest{Path: r.URL.Path, Header: r.Header.Clone()}
	switch {
	case strings.HasPrefix(r.URL.Path, "/api/"):
		if err := r.ParseForm(); err != nil {
			f.t.Errorf("parse form: %v", err)
		}
		req.Form = r.PostForm
		f.record(req)
		_, _ = io.WriteString(w, f.answers[strings.TrimPrefix(r.URL.Path, "/api/")])
	case strings.HasPrefix(r.URL.Path, "/upload/"):
		body, _ := io.ReadAll(r.Body)
		req.Body = string(body)
		f.record(req)
		_, _ = io.WriteString(w, "OK - "+r.URL.Path)
	default:
		f.record(req)
		f.download(w)
	}
}

func (f *filesSlack) record(r fileRequest) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r)
}

func (f *filesSlack) recorded() []fileRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests
}

func upload(name, content string) provider.Upload {
	return provider.Upload{Name: name, Size: int64(len(content)), Content: strings.NewReader(content)}
}

func TestUploadFilesUsesTheExternalUploadFlow(t *testing.T) {
	api, f := newFilesSlack(t, domain.Files)
	f.answers["files.getUploadURLExternal"] = `{"ok":true,"upload_url":"` + f.srv.URL + `/upload/v1/abc","file_id":"F0001"}`
	f.answers["files.completeUploadExternal"] = `{"ok":true,"files":[{"id":"F0001","name":"notes.txt","title":"notes.txt","mimetype":"text/plain","size":5,
		"url_private_download":"https://files.slack.com/files-pri/T0001-F0001/download/notes.txt"}]}`
	conv, thread := must(t, domain.ParseConversationID, "C0001"), must(t, domain.ParseTS, "1700000100.000100")

	got, err := api.UploadFiles(context.Background(), conv, thread, []provider.Upload{upload("notes.txt", "hello")}, "see notes")
	if err != nil {
		t.Fatal(err)
	}

	want := []domain.File{{ID: must(t, domain.ParseFileID, "F0001"), Name: "notes.txt", Title: "notes.txt", MIMEType: "text/plain", Size: 5,
		DownloadURL: "https://files.slack.com/files-pri/T0001-F0001/download/notes.txt"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("UploadFiles() = %+v, want %+v", got, want)
	}
	reqs := f.recorded()
	if len(reqs) != 3 {
		t.Fatalf("requests = %+v, want 3", reqs)
	}
	if want := (url.Values{"filename": {"notes.txt"}, "length": {"5"}, "token": {"xoxc-secret"}}); !reflect.DeepEqual(reqs[0].Form, want) {
		t.Errorf("getUploadURLExternal form = %v, want %v", reqs[0].Form, want)
	}
	// The upload URL is pre-signed: no credentials go to it.
	if reqs[1].Path != "/upload/v1/abc" || reqs[1].Body != "hello" || reqs[1].Header.Get("Cookie") != "" || reqs[1].Header.Get("Authorization") != "" {
		t.Errorf("upload = %+v, want the bytes and no credentials", reqs[1])
	}
	wantComplete := url.Values{"files": {`[{"id":"F0001","title":"notes.txt"}]`}, "channel_id": {"C0001"},
		"thread_ts": {"1700000100.000100"}, "initial_comment": {"see notes"}, "token": {"xoxc-secret"}}
	if !reflect.DeepEqual(reqs[2].Form, wantComplete) {
		t.Errorf("completeUploadExternal form = %v, want %v", reqs[2].Form, wantComplete)
	}
}

func TestUploadingSeveralFilesSharesThemInOneMessage(t *testing.T) {
	api, f := newFilesSlack(t, domain.Files)
	f.answers["files.getUploadURLExternal"] = `{"ok":true,"upload_url":"` + f.srv.URL + `/upload/v1/x","file_id":"F0001"}`
	f.answers["files.completeUploadExternal"] = `{"ok":true,"files":[]}`

	files := []provider.Upload{upload("a.txt", "a"), upload("b.txt", "bb")}
	if _, err := api.UploadFiles(context.Background(), must(t, domain.ParseConversationID, "C0001"), domain.TS{}, files, ""); err != nil {
		t.Fatal(err)
	}
	reqs := f.recorded()
	if len(reqs) != 5 {
		t.Fatalf("requests = %d, want two URLs, two uploads and one completion", len(reqs))
	}
	complete := reqs[4].Form
	if complete.Has("thread_ts") || complete.Has("initial_comment") {
		t.Errorf("completion form = %v, want no thread and no caption", complete)
	}
	if got := complete.Get("files"); got != `[{"id":"F0001","title":"a.txt"},{"id":"F0001","title":"b.txt"}]` {
		t.Errorf("files = %s", got)
	}
}

func TestAFailedUploadStopsBeforeSharing(t *testing.T) {
	api, f := newFilesSlack(t, domain.Files)
	f.answers["files.getUploadURLExternal"] = `{"ok":true,"upload_url":"` + f.srv.URL + `/nowhere","file_id":"F0001"}`
	f.download = func(w http.ResponseWriter) { w.WriteHeader(http.StatusInternalServerError) }

	_, err := api.UploadFiles(context.Background(), must(t, domain.ParseConversationID, "C0001"), domain.TS{}, []provider.Upload{upload("a", "a")}, "")
	if err == nil || len(f.recorded()) != 2 {
		t.Fatalf("UploadFiles() err = %v after %d requests, want an error and no completion", err, len(f.recorded()))
	}
}

func TestUploadErrorsComeFromSlack(t *testing.T) {
	api, f := newFilesSlack(t, domain.Files)
	f.answers["files.getUploadURLExternal"] = `{"ok":false,"error":"invalid_auth"}`
	_, err := api.UploadFiles(context.Background(), must(t, domain.ParseConversationID, "C0001"), domain.TS{}, []provider.Upload{upload("a", "a")}, "")
	if !errors.Is(err, provider.ErrAuthExpired) {
		t.Fatalf("UploadFiles() err = %v, want ErrAuthExpired", err)
	}
}

func TestFilesNeedTheFilesCapability(t *testing.T) {
	api, f := newFilesSlack(t)
	conv := must(t, domain.ParseConversationID, "C0001")
	if _, err := api.UploadFiles(context.Background(), conv, domain.TS{}, []provider.Upload{upload("a", "a")}, ""); !errors.Is(err, domain.ErrUnsupported) {
		t.Errorf("UploadFiles() err = %v, want ErrUnsupported", err)
	}
	if _, err := api.DownloadFile(context.Background(), domain.File{DownloadURL: f.srv.URL + "/files-pri/x"}, io.Discard, 10); !errors.Is(err, domain.ErrUnsupported) {
		t.Errorf("DownloadFile() err = %v, want ErrUnsupported", err)
	}
	if n := len(f.recorded()); n != 0 {
		t.Errorf("%d requests reached Slack", n)
	}
}

func TestDownloadFileStreamsWithTheCredentialsInHeaders(t *testing.T) {
	api, f := newFilesSlack(t, domain.Files)
	f.download = func(w http.ResponseWriter) { _, _ = io.WriteString(w, "PNG bytes") }

	var out bytes.Buffer
	n, err := api.DownloadFile(context.Background(), domain.File{DownloadURL: f.srv.URL + "/files-pri/T0001-F0001/download/a.png"}, &out, 100)
	if err != nil || n != 9 || out.String() != "PNG bytes" {
		t.Fatalf("DownloadFile() = %d, %v, %q", n, err, out.String())
	}
	h := f.recorded()[0].Header
	// The files host reads no form: a token signed into the form goes in
	// the Authorization header, with every header the signer set.
	if h.Get("Authorization") != "Bearer xoxc-secret" || h.Get("Cookie") != "d=cookie" {
		t.Errorf("headers = %v, want the bearer token and the cookie", h)
	}
}

func TestDownloadFileRefusesWhatIsTooLarge(t *testing.T) {
	api, f := newFilesSlack(t, domain.Files)
	file := domain.File{DownloadURL: f.srv.URL + "/files-pri/x"}

	// Declared too large up front.
	f.download = func(w http.ResponseWriter) { _, _ = io.WriteString(w, "0123456789") }
	if _, err := api.DownloadFile(context.Background(), file, io.Discard, 5); !errors.Is(err, provider.ErrFileTooLarge) {
		t.Errorf("declared length: err = %v, want ErrFileTooLarge", err)
	}
	// Streamed without a length: cut off past the limit.
	f.download = func(w http.ResponseWriter) {
		w.(http.Flusher).Flush()
		_, _ = io.WriteString(w, "0123456789")
	}
	var out bytes.Buffer
	if _, err := api.DownloadFile(context.Background(), file, &out, 5); !errors.Is(err, provider.ErrFileTooLarge) || out.Len() > 6 {
		t.Errorf("streamed: err = %v after %d bytes, want ErrFileTooLarge within max+1", err, out.Len())
	}
}

func TestDownloadFileFailures(t *testing.T) {
	api, f := newFilesSlack(t, domain.Files)
	ctx := context.Background()
	if _, err := api.DownloadFile(ctx, domain.File{}, io.Discard, 5); !errors.Is(err, provider.ErrNotDownloadable) {
		t.Errorf("no URL: err = %v, want ErrNotDownloadable", err)
	}
	// Credentials only ever go to Slack's own hosts.
	for _, u := range []string{"https://evil.example/files-pri/x", "http://files.slack.com/x", "https://files.slack.com.evil.example/x", "::"} {
		if _, err := api.DownloadFile(ctx, domain.File{DownloadURL: u}, io.Discard, 5); err == nil {
			t.Errorf("%s: no error", u)
		}
	}
	f.download = func(w http.ResponseWriter) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
	}
	var limited *provider.RateLimitedError
	if _, err := api.DownloadFile(ctx, domain.File{DownloadURL: f.srv.URL + "/files-pri/x"}, io.Discard, 5); !errors.As(err, &limited) {
		t.Errorf("429: err = %v, want RateLimitedError", err)
	}
	f.download = func(w http.ResponseWriter) { w.WriteHeader(http.StatusNotFound) }
	if _, err := api.DownloadFile(ctx, domain.File{DownloadURL: f.srv.URL + "/files-pri/x"}, io.Discard, 5); err == nil {
		t.Error("404: no error")
	}
	// Signed out, the files host answers with the sign-in page.
	f.download = func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<html>")
	}
	if _, err := api.DownloadFile(ctx, domain.File{MIMEType: "image/png", DownloadURL: f.srv.URL + "/files-pri/x"}, io.Discard, 50); err == nil {
		t.Error("HTML for a PNG: no error")
	}
}

func TestTrustedHosts(t *testing.T) {
	api, f := newFilesSlack(t, domain.Files)
	for raw, want := range map[string]bool{
		"https://files.slack.com/files-pri/x": true,
		"https://slack.com/x":                 true,
		f.srv.URL + "/files-pri/x":            true, // the Web API's own host
		"http://files.slack.com/x":            false,
		"https://slack.com.evil.example/x":    false,
		"https://evilslack.com/x":             false,
	} {
		u, _ := url.Parse(raw)
		if got := api.trusts(u); got != want {
			t.Errorf("trusts(%s) = %t, want %t", raw, got, want)
		}
	}
}
