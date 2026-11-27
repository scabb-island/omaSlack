package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"gitlab.com/scabb-island/omaslack/internal/rpc"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// Files (spec messaging, "Files and images"): o opens a message's file in
// the desktop's default app, :attach <path> [caption] uploads one. Both go
// through the daemon: files.fetch downloads into its cache and answers the
// local path, files.upload reads the path on this machine. Workspaces
// without the files capability do neither.

// Opener opens a local file in the desktop's default app. It must not wait
// for the app to close.
type Opener func(path string) error

// xdgOpen starts xdg-open on path and reaps it in the background. Its
// output is dropped: it would land on the TUI's screen.
func xdgOpen(path string) error {
	cmd := exec.CommandContext(context.Background(), "xdg-open", path) // #nosec G204 -- a path in the daemon's cache, passed as one argument
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("xdg-open: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// filesCapability is the workspace capability files.fetch and
// files.upload need.
const filesCapability = "files"

func (m *Model) canFiles() bool {
	return slices.Contains(m.st.Workspace(m.team).Capabilities, filesCapability)
}

// fileKey names a file across workspaces.
type fileKey struct{ team, id string }

// fetchResult is files.fetch's answer.
type fetchResult struct {
	LocalPath string `json:"localPath"`
}

// fetchFile calls files.fetch off the Update loop; then receives the
// local path or the error inside Update.
func (m *Model) fetchFile(fk fileKey, then func(*Model, string, error) tea.Cmd) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), CallTimeout)
		defer cancel()
		var r fetchResult
		raw, err := c.Call(ctx, "files.fetch", map[string]any{"team": fk.team, "fileId": fk.id})
		if err == nil {
			err = json.Unmarshal(raw, &r)
		}
		return resultMsg{method: "files.fetch", apply: func(m *Model) tea.Cmd { return then(m, r.LocalPath, err) }}
	}
}

// selectedFiles are the files of the selected message in the focused
// pane (the newest when none is selected).
func (m *Model) selectedFiles() []wire.File {
	msgs, sel := m.messages(), m.msgSel
	if m.focus == threadPane {
		msgs, sel = m.replies(), m.threadSel
	}
	if len(msgs) == 0 {
		return nil
	}
	msg := msgs[len(msgs)-1]
	for _, x := range msgs {
		if x.TS == sel {
			msg = x
		}
	}
	return msg.Files
}

// openFile opens the selected message's file, or offers a choice when it
// has several.
func (m *Model) openFile() tea.Cmd {
	files := m.selectedFiles()
	switch {
	case len(files) == 0:
		m.setError("This message has no files.")
		return nil
	case !m.canFiles():
		m.setError("This workspace cannot download files (no files capability).")
		return nil
	case len(files) > 1:
		m.media.chooser = &chooser{files: files}
		return nil
	}
	return m.openOne(files[0])
}

// openOne downloads f through the daemon and opens it.
func (m *Model) openOne(f wire.File) tea.Cmd {
	name := fileName(f)
	m.setInfo("Opening " + name + "…")
	return m.fetchFile(fileKey{m.team, f.ID}, func(m *Model, path string, err error) tea.Cmd {
		if err != nil {
			m.setError(fileError("download "+name, err))
			return nil
		}
		return m.launch(name, path)
	})
}

// launch runs the opener off the Update loop.
func (m *Model) launch(name, path string) tea.Cmd {
	open := m.media.open
	return func() tea.Msg {
		err := open(path)
		return resultMsg{method: "open", apply: func(m *Model) tea.Cmd {
			if err != nil {
				m.setError("Cannot open " + name + ": " + err.Error())
				return nil
			}
			m.setInfo("Opened " + name)
			return nil
		}}
	}
}

func fileName(f wire.File) string {
	for _, n := range []string{f.Name, f.Title, f.ID} {
		if n != "" {
			return n
		}
	}
	return "the file"
}

// fileError words a files.* failure: -32602 is a file the daemon cannot
// use (it says why), -32002 a workspace without the capability.
func fileError(what string, err error) string {
	var e *rpc.Error
	switch {
	case errors.As(err, &e) && e.Code == rpc.CodeInvalidParams:
		return "Cannot " + what + ": the file cannot be downloaded or read (" + e.Message + ")"
	case errors.As(err, &e) && e.Code == rpc.CodeUnsupportedCapability:
		return "Cannot " + what + ": this workspace has no files capability"
	}
	return "Cannot " + what + ": " + err.Error()
}

// chooser picks one of a message's files to open.
type chooser struct {
	files []wire.File
	sel   int
}

// chooserKey moves the choice (j/k), opens it (Enter or its number 1–9)
// or, on any other key, closes the chooser.
func (m *Model) chooserKey(k tea.KeyPressMsg) tea.Cmd {
	c := m.media.chooser
	switch {
	case key.Matches(k, m.keys.Down):
		c.sel = min(c.sel+1, len(c.files)-1)
	case key.Matches(k, m.keys.Up):
		c.sel = max(c.sel-1, 0)
	case key.Matches(k, m.keys.Open):
		return m.choose(c.sel)
	case key.Matches(k, m.keys.Workspace):
		return m.choose(int(k.Code - '1'))
	default:
		m.media.chooser = nil
	}
	return nil
}

// choose opens the i-th file of the chooser, and closes it.
func (m *Model) choose(i int) tea.Cmd {
	c := m.media.chooser
	m.media.chooser = nil
	if i >= len(c.files) {
		return nil
	}
	return m.openOne(c.files[i])
}

// chooserView lists the files, numbered, over the panes.
func (m *Model) chooserView(h int) string {
	lines := []string{"Open which file?", ""}
	for i, f := range m.media.chooser.files {
		line := fmt.Sprintf(" %d  %s", i+1, fileName(f))
		if i == m.media.chooser.sel {
			line = lipgloss.NewStyle().Foreground(m.theme.Accent).Background(m.theme.Selection).Render(line)
		}
		lines = append(lines, line)
	}
	lines = append(lines, "", m.muted("j/k move · Enter or 1–9 open · Esc cancel"))
	w := min(m.width, 56)
	box := m.box("Files", lines, w, min(h, len(lines)+2), true)
	return lipgloss.Place(m.width, h, lipgloss.Center, lipgloss.Center, box)
}

// startAttach leaves INSERT for the command line, ready for :attach.
func (m *Model) startAttach() tea.Cmd {
	if !m.canFiles() {
		m.setError("This workspace cannot send files (no files capability).")
		return nil
	}
	m.mode, m.cmdline = commandMode, "attach "
	m.compose.Blur()
	return m.saveDraft()
}

// uploadResult is files.upload's answer.
type uploadResult struct {
	Files []wire.File `json:"files"`
}

// attach runs ":attach <path> [caption]": it uploads the file to the open
// conversation, or to the thread when the thread pane has the focus. A
// path with spaces goes in quotes; ~ is the home directory and a relative
// path starts at the TUI's working directory, because the daemon needs an
// absolute one.
func (m *Model) attach(arg string) tea.Cmd {
	path, caption := splitPath(strings.TrimSpace(arg))
	switch {
	case path == "":
		m.setError("Usage: :attach <path> [caption]")
		return nil
	case m.openConv() == "":
		m.setError("Open a conversation first (Enter).")
		return nil
	case !m.canFiles():
		m.setError("This workspace cannot send files (no files capability).")
		return nil
	}
	abs, err := absPath(path)
	if err != nil {
		m.setError("Cannot attach " + path + ": " + err.Error())
		return nil
	}
	return m.upload(abs, caption)
}

func (m *Model) upload(path, caption string) tea.Cmd {
	t := target{m.team, m.openConv(), ""}
	if m.focus == threadPane {
		t.thread = m.thread
	}
	extra := []any{"paths", []string{path}}
	if caption != "" {
		extra = append(extra, "caption", caption)
	}
	name := filepath.Base(path)
	m.setInfo("Uploading " + name + "…")
	c := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), CallTimeout)
		defer cancel()
		var r uploadResult
		raw, err := c.Call(ctx, "files.upload", t.params(extra...))
		if err == nil {
			err = json.Unmarshal(raw, &r)
		}
		return resultMsg{method: "files.upload", apply: func(m *Model) tea.Cmd {
			m.uploaded(name, r, err)
			return nil
		}}
	}
}

func (m *Model) uploaded(name string, r uploadResult, err error) {
	if err != nil {
		m.setError(fileError("attach "+name, err))
		return
	}
	names := make([]string, 0, len(r.Files))
	for _, f := range r.Files {
		names = append(names, fileName(f))
	}
	m.setInfo("Sent " + strings.Join(names, ", "))
}

// splitPath splits ":attach"'s argument into the path, in double or
// single quotes when it has spaces, and the caption after it.
func splitPath(arg string) (path, caption string) {
	if arg != "" && (arg[0] == '"' || arg[0] == '\'') {
		if end := strings.IndexByte(arg[1:], arg[0]); end >= 0 {
			return arg[1 : end+1], strings.TrimSpace(arg[end+2:])
		}
	}
	path, caption, _ = strings.Cut(arg, " ")
	return path, strings.TrimSpace(caption)
}

// absPath expands ~ and makes path absolute from the working directory.
func absPath(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, path[1:])
	}
	return filepath.Abs(path)
}
