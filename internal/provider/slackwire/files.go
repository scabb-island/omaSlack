package slackwire

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
)

// The files behind provider.FileUploader and provider.FileDownloader.
//
// Uploads use Slack's current flow (files.upload is deprecated):
// files.getUploadURLExternal for each file, a POST of its bytes to the
// pre-signed upload URL it returns (no credentials go there), and one
// files.completeUploadExternal that shares every file in one message.
//
// Downloads GET the file's url_private_download with the provider's own
// credentials. The files host reads no form, so a token a Signer puts in
// the form (the desktop-session xoxc token) goes in the Authorization
// header instead, next to every header the Signer sets (its cookies).
// Credentials only go to Slack's own hosts over HTTPS, or to the Web
// API's host.

// methodDownload names downloads in errors and rate limits; it is not a
// Web API method.
const methodDownload = "files.download"

type uploadURLResponse struct {
	Envelope
	UploadURL string `json:"upload_url"`
	FileID    string `json:"file_id"`
}

type completeUploadResponse struct {
	Envelope
	Files []File `json:"files"`
}

// fileRef is one uploaded file, as files.completeUploadExternal takes it.
type fileRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// UploadFiles shares files in conv (inside thread when it is not zero)
// as one message whose text is caption.
func (a *WebAPI) UploadFiles(ctx context.Context, conv domain.ConversationID, thread domain.TS, files []provider.Upload, caption string) ([]domain.File, error) {
	if err := a.caps.Require(domain.Files); err != nil {
		return nil, fmt.Errorf("files.completeUploadExternal: %w", err)
	}
	refs := make([]fileRef, 0, len(files))
	for _, f := range files {
		id, err := a.uploadOne(ctx, f)
		if err != nil {
			return nil, err
		}
		refs = append(refs, fileRef{ID: id, Title: f.Name})
	}
	return a.completeUpload(ctx, conv, thread, refs, caption)
}

// uploadOne sends one file's bytes and returns the ID Slack gave it.
func (a *WebAPI) uploadOne(ctx context.Context, f provider.Upload) (string, error) {
	form := url.Values{"filename": {f.Name}, "length": {strconv.FormatInt(f.Size, 10)}}
	var resp uploadURLResponse
	if err := a.Call(ctx, "files.getUploadURLExternal", form, &resp); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resp.UploadURL, io.LimitReader(f.Content, f.Size))
	if err != nil {
		return "", fmt.Errorf("upload %s: %w", f.Name, err)
	}
	req.ContentLength = f.Size
	req.Header.Set("Content-Type", "application/octet-stream")
	sent, err := a.send(req, "upload "+f.Name)
	if err != nil {
		return "", err
	}
	_ = sent.Body.Close()
	return resp.FileID, nil
}

func (a *WebAPI) completeUpload(ctx context.Context, conv domain.ConversationID, thread domain.TS, refs []fileRef, caption string) ([]domain.File, error) {
	list, err := json.Marshal(refs)
	if err != nil {
		return nil, fmt.Errorf("files.completeUploadExternal: %w", err)
	}
	form := url.Values{"files": {string(list)}, "channel_id": {conv.String()}}
	if !thread.IsZero() {
		form.Set("thread_ts", thread.String())
	}
	if caption != "" {
		form.Set("initial_comment", caption)
	}
	var resp completeUploadResponse
	if err := a.Call(ctx, "files.completeUploadExternal", form, &resp); err != nil {
		return nil, err
	}
	return parseFiles(resp.Files), nil
}

// errWebPage reports a download answered with a web page instead of the
// file: what the files host does for credentials it no longer accepts.
var errWebPage = errors.New("the files host answered with a web page, not the file")

// DownloadFile streams f into w, refusing more than max bytes.
func (a *WebAPI) DownloadFile(ctx context.Context, f domain.File, w io.Writer, max int64) (int64, error) {
	req, err := a.downloadRequest(ctx, f)
	if err != nil {
		return 0, err
	}
	resp, err := a.send(req, methodDownload)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if err := checkDownload(resp, f, max); err != nil {
		return 0, err
	}
	n, err := io.Copy(w, io.LimitReader(resp.Body, max+1))
	if err != nil {
		return n, fmt.Errorf("%s: %w", methodDownload, err)
	}
	if n > max {
		return n, fmt.Errorf("%s: %w", methodDownload, provider.ErrFileTooLarge)
	}
	return n, nil
}

// downloadRequest is the signed GET of f's download URL.
func (a *WebAPI) downloadRequest(ctx context.Context, f domain.File) (*http.Request, error) {
	if err := a.caps.Require(domain.Files); err != nil {
		return nil, fmt.Errorf("%s: %w", methodDownload, err)
	}
	if f.DownloadURL == "" {
		return nil, fmt.Errorf("%s %s: %w", methodDownload, f.ID, provider.ErrNotDownloadable)
	}
	u, err := url.Parse(f.DownloadURL)
	if err != nil || !a.trusts(u) {
		return nil, fmt.Errorf("%s %s: refusing to send credentials to %q", methodDownload, f.ID, f.DownloadURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", methodDownload, err)
	}
	a.signHeaders(req.Header)
	return req, nil
}

// signHeaders signs a request that carries no form: a token the Signer
// put in the form becomes a bearer token.
func (a *WebAPI) signHeaders(h http.Header) {
	form := url.Values{}
	a.sign(form, h)
	if token := form.Get("token"); token != "" {
		h.Set("Authorization", "Bearer "+token)
	}
}

// trusts reports whether u may receive the provider's credentials:
// Slack's own hosts over HTTPS, or the Web API's own host (which tests
// point at a local server).
func (a *WebAPI) trusts(u *url.URL) bool {
	if base, err := url.Parse(a.url); err == nil && u.Scheme == base.Scheme && u.Host == base.Host {
		return true
	}
	host := u.Hostname()
	return u.Scheme == "https" && (host == "slack.com" || strings.HasSuffix(host, ".slack.com"))
}

// send does req and checks its status: 200 is the only success, and a
// 429 is a *provider.RateLimitedError for method. On success the caller
// closes the body.
func (a *WebAPI) send(req *http.Request, method string) (*http.Response, error) {
	resp, err := a.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return resp, nil
	case http.StatusTooManyRequests:
		_ = resp.Body.Close()
		return nil, &provider.RateLimitedError{Method: method, RetryAfter: retryAfter(resp.Header)}
	default:
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%s: HTTP %s", method, resp.Status)
	}
}

// checkDownload refuses a sign-in page served instead of a file, and a
// file that declares itself larger than max.
func checkDownload(resp *http.Response, f domain.File, max int64) error {
	if mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); mt == "text/html" && f.MIMEType != "text/html" {
		return fmt.Errorf("%s %s: %w", methodDownload, f.ID, errWebPage)
	}
	if resp.ContentLength > max {
		return fmt.Errorf("%s %s: %w", methodDownload, f.ID, provider.ErrFileTooLarge)
	}
	return nil
}
