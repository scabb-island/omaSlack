package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"gitlab.com/scabb-island/omaslack/internal/rpc"
	"gitlab.com/scabb-island/omaslack/internal/tui/client"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// The files tests run against a daemon whose Acme workspace has the files
// capability, with #general's newest message carrying files.

func report() wire.File {
	return wire.File{ID: "F1", Name: "report.pdf", MIMEType: "application/pdf", Size: 2048}
}

func notes() wire.File {
	return wire.File{ID: "F2", Name: "notes.txt", MIMEType: "text/plain", Size: 12}
}

// filesDaemon is daemon with the files capability on Acme, and #general's
// newest message carrying files.
func filesDaemon(t *testing.T, files []wire.File, handlers map[string]client.Handler) *client.Fake {
	t.Helper()
	msgs := generalMessages()
	msgs[0].Files = files
	h := map[string]client.Handler{
		"conversations.open": client.Result(openResult{Messages: msgs}),
		"files.fetch": func(p json.RawMessage) (any, error) {
			var req struct{ FileID string }
			_ = json.Unmarshal(p, &req)
			return map[string]string{"localPath": "/cache/" + req.FileID}, nil
		},
		"files.upload": client.Result(map[string]any{"files": []wire.File{report()}}),
	}
	for k, v := range handlers {
		h[k] = v
	}
	f := daemon(h)
	withFiles := acme()
	withFiles.Capabilities = []string{"realtime", "files"}
	raw, err := json.Marshal(hello(withFiles, wire.Workspace{Team: "T2", Name: "Beta", State: "connected"}))
	if err != nil {
		t.Fatal(err)
	}
	f.HelloResult = raw
	return f
}

// opened records what the fake opener was asked to open.
type opened struct {
	paths []string
	err   error
}

func (o *opened) open(path string) error {
	o.paths = append(o.paths, path)
	return o.err
}

// openGeneral opens #general with a fake opener.
func openGeneralWithFiles(t *testing.T, f *client.Fake) (*harness, *opened) {
	t.Helper()
	h := newHarness(t, f, 120, 40)
	o := &opened{}
	h.m.media.open = o.open
	h.keys(":")
	h.typeText("open general")
	h.keys("enter")
	return h, o
}

func TestOpenFileFetchesItAndOpensTheLocalPath(t *testing.T) {
	h, o := openGeneralWithFiles(t, filesDaemon(t, []wire.File{report()}, nil))
	h.keys("o")
	if p := h.lastCall("files.fetch"); p["team"] != "T1" || p["fileId"] != "F1" {
		t.Fatalf("files.fetch %v", p)
	}
	if len(o.paths) != 1 || o.paths[0] != "/cache/F1" {
		t.Fatalf("opened %v", o.paths)
	}
	if h.m.statusIsError || !strings.Contains(h.m.status, "report.pdf") {
		t.Fatalf("status %q", h.m.status)
	}
}

func TestOpenFileOfTheSelectedMessage(t *testing.T) {
	msgs := generalMessages()
	msgs[1].Files = []wire.File{notes()}
	h, o := openGeneralWithFiles(t, filesDaemon(t, nil, map[string]client.Handler{
		"conversations.open": client.Result(openResult{Messages: msgs}),
	}))
	h.keys("k", "o") // "standup at 10?"
	if len(o.paths) != 1 || o.paths[0] != "/cache/F2" {
		t.Fatalf("opened %v", o.paths)
	}
}

func TestOpenFileWithoutFiles(t *testing.T) {
	h, o := openGeneralWithFiles(t, filesDaemon(t, nil, nil))
	h.keys("o")
	if !h.m.statusIsError || h.count("files.fetch") != 0 || len(o.paths) != 0 {
		t.Fatalf("status %q, calls %v", h.m.status, h.calls())
	}
}

func TestOpenFileThatCannotBeDownloaded(t *testing.T) {
	h, o := openGeneralWithFiles(t, filesDaemon(t, []wire.File{report()}, map[string]client.Handler{
		"files.fetch": func(json.RawMessage) (any, error) {
			return nil, &rpc.Error{Code: rpc.CodeInvalidParams, Message: "fileId: Slack offers no download for this file"}
		},
	}))
	h.keys("o")
	if !h.m.statusIsError || !strings.Contains(h.m.status, "cannot be downloaded") || len(o.paths) != 0 {
		t.Fatalf("status %q, opened %v", h.m.status, o.paths)
	}
}

func TestOpenFileWhenTheOpenerFails(t *testing.T) {
	h, o := openGeneralWithFiles(t, filesDaemon(t, []wire.File{report()}, nil))
	o.err = errors.New("xdg-open: not found")
	h.keys("o")
	if !h.m.statusIsError || !strings.Contains(h.m.status, "xdg-open: not found") {
		t.Fatalf("status %q", h.m.status)
	}
}

func TestOpenFileWithoutTheFilesCapability(t *testing.T) {
	msgs := generalMessages()
	msgs[0].Files = []wire.File{report()}
	h := newHarness(t, daemon(map[string]client.Handler{"conversations.open": client.Result(openResult{Messages: msgs})}), 120, 40)
	h.keys(":")
	h.typeText("open general")
	h.keys("enter", "o")
	if !h.m.statusIsError || h.count("files.fetch") != 0 {
		t.Fatalf("status %q, calls %v", h.m.status, h.calls())
	}
}

func TestOpenFileChooser(t *testing.T) {
	h, o := openGeneralWithFiles(t, filesDaemon(t, []wire.File{report(), notes()}, nil))
	h.keys("o")
	if h.count("files.fetch") != 0 || !strings.Contains(h.screen(), "Open which file?") {
		t.Fatalf("no chooser, calls %v:\n%s", h.calls(), h.screen())
	}
	h.keys("j", "enter")
	if len(o.paths) != 1 || o.paths[0] != "/cache/F2" {
		t.Fatalf("opened %v", o.paths)
	}
	if strings.Contains(h.screen(), "Open which file?") {
		t.Fatalf("chooser still open:\n%s", h.screen())
	}
	h.keys("o", "1")
	if len(o.paths) != 2 || o.paths[1] != "/cache/F1" {
		t.Fatalf("opened %v", o.paths)
	}
	h.keys("o", "esc", "o", "9", "esc")
	if len(o.paths) != 2 || h.m.media.chooser != nil {
		t.Fatalf("opened %v, chooser %v", o.paths, h.m.media.chooser)
	}
}

func TestAttachUploadsAnAbsolutePathWithCaption(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	h, _ := openGeneralWithFiles(t, filesDaemon(t, nil, nil))
	h.keys(":")
	h.typeText("attach ~/cat.png look at this")
	h.keys("enter")
	p := h.lastCall("files.upload")
	if p["team"] != "T1" || p["conv"] != "C1" || p["caption"] != "look at this" || p["threadTS"] != nil {
		t.Fatalf("files.upload %v", p)
	}
	if paths := p["paths"].([]any); len(paths) != 1 || paths[0] != filepath.Join(home, "cat.png") {
		t.Fatalf("paths %v", paths)
	}
	if h.m.statusIsError || !strings.Contains(h.m.status, "report.pdf") {
		t.Fatalf("status %q", h.m.status)
	}
}

func TestAttachRelativeAndQuotedPaths(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	h, _ := openGeneralWithFiles(t, filesDaemon(t, nil, nil))
	h.keys(":")
	h.typeText(`attach "my notes.txt"`)
	h.keys("enter")
	p := h.lastCall("files.upload")
	if paths := p["paths"].([]any); paths[0] != filepath.Join(cwd, "my notes.txt") || p["caption"] != nil {
		t.Fatalf("files.upload %v", p)
	}
}

func TestAttachInTheThread(t *testing.T) {
	h, _ := openGeneralWithFiles(t, filesDaemon(t, nil, nil))
	h.keys("enter", ":") // the thread of the newest message, focused
	h.typeText("attach /tmp/a.png")
	h.keys("enter")
	if p := h.lastCall("files.upload"); p["threadTS"] != tsC {
		t.Fatalf("files.upload %v", p)
	}
}

func TestAttachFromInsertMode(t *testing.T) {
	h, _ := openGeneralWithFiles(t, filesDaemon(t, nil, nil))
	h.keys("i")
	h.typeText("draft")
	h.send(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if h.m.mode != commandMode || h.m.cmdline != "attach " {
		t.Fatalf("mode %v, cmdline %q", h.m.mode, h.m.cmdline)
	}
	if h.m.st.Draft(target{"T1", "C1", ""}) != "draft" {
		t.Fatal("the draft was not saved")
	}
}

func TestAttachErrors(t *testing.T) {
	h, _ := openGeneralWithFiles(t, filesDaemon(t, nil, map[string]client.Handler{
		"files.upload": func(json.RawMessage) (any, error) {
			return nil, &rpc.Error{Code: rpc.CodeInvalidParams, Message: "paths: /tmp/nope does not exist"}
		},
	}))
	h.keys(":")
	h.typeText("attach")
	h.keys("enter")
	if !h.m.statusIsError || !strings.Contains(h.m.status, "Usage") {
		t.Fatalf("status %q", h.m.status)
	}
	h.keys(":")
	h.typeText("attach /tmp/nope")
	h.keys("enter")
	if !h.m.statusIsError || !strings.Contains(h.m.status, "does not exist") {
		t.Fatalf("status %q", h.m.status)
	}
}

func TestAttachWithoutTheFilesCapability(t *testing.T) {
	h := newHarness(t, daemon(nil), 120, 40)
	h.keys(":")
	h.typeText("open general")
	h.keys("enter", ":")
	h.typeText("attach /tmp/a.png")
	h.keys("enter")
	if !h.m.statusIsError || !strings.Contains(h.m.status, "files") || h.count("files.upload") != 0 {
		t.Fatalf("status %q, calls %v", h.m.status, h.calls())
	}
	h.keys("i")
	h.send(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if h.m.mode != insertMode || !h.m.statusIsError {
		t.Fatalf("mode %v, status %q", h.m.mode, h.m.status)
	}
}
