package app

import (
	"encoding/json"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"gitlab.com/scabb-island/omaslack/internal/tui/client"
	"gitlab.com/scabb-island/omaslack/internal/tui/image"
	"gitlab.com/scabb-island/omaslack/internal/tui/theme"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// mode is the vim-like input mode.
type mode int

const (
	normalMode mode = iota
	insertMode
	commandMode
)

func (m mode) String() string { return [...]string{"NORMAL", "INSERT", "COMMAND"}[m] }

// pane is a pane that takes the focus in NORMAL mode, left to right.
type pane int

const (
	sidebarPane pane = iota
	messagesPane
	threadPane
)

// sidebarPref is whether the user asked for the sidebar. Auto shows it
// from narrowWidth columns up.
type sidebarPref int

const (
	sidebarAuto sidebarPref = iota
	sidebarShown
	sidebarHidden
)

// userKey names a user across workspaces.
type userKey struct{ team, user string }

// Model is the root bubbletea model. Everything it shows comes from st;
// the rest is view state: mode, focus, selections, compose.
type Model struct {
	client client.Client
	st     *State
	keys   Keys
	theme  theme.Theme
	images image.Protocol
	now    func() time.Time
	loc    *time.Location

	width, height int
	mode          mode
	focus         pane
	team          string            // the active workspace
	sel           map[string]string // sidebar selection per workspace (a conversation ID)
	open          map[string]string // open conversation per workspace
	msgSel        string            // selected message; "" follows the newest
	thread        string            // open thread's root; "" for none
	threadSel     string            // selected reply; "" follows the newest
	sidebar       sidebarPref
	sidebarWidth  int
	compose       textarea.Model
	target        target // where compose writes
	cmdline       string
	help          bool
	status        string
	statusIsError bool
	gone          bool // the daemon connection ended

	marked     map[convKey]string    // newest TS already sent to conversations.markRead
	asked      map[userKey]time.Time // when users.info was last asked about a user
	without    map[string]bool       // optional methods the daemon cannot do
	lastTyping time.Time
	actions    []action

	cx composeState // autocomplete, mention tokens, edit and delete (complete.go, edit.go)
}

// action is what a NORMAL mode key does.
type action struct {
	keys key.Binding
	do   func(m *Model, k tea.KeyPressMsg) tea.Cmd
}

var _ tea.Model = (*Model)(nil)

func newModel(o Options) *Model {
	st, err := NewState(o.Client.Hello())
	m := &Model{
		client: o.Client, st: st, keys: DefaultKeys(), theme: o.Theme, images: o.Images,
		now: o.Now, loc: o.Loc, sidebarWidth: defaultSidebarWidth,
		sel: map[string]string{}, open: map[string]string{},
		marked: map[convKey]string{}, asked: map[userKey]time.Time{}, without: map[string]bool{},
	}
	if m.now == nil {
		m.now = time.Now
	}
	if err != nil {
		m.st, _ = NewState(json.RawMessage(`{}`))
		m.setError(err.Error())
	}
	if teams := m.st.Teams(); len(teams) > 0 {
		m.team = teams[0]
	}
	m.compose = newCompose()
	m.applyTheme()
	m.actions = m.normalActions()
	return m
}

func newCompose() textarea.Model {
	ta := textarea.New()
	ta.Prompt = ""
	ta.ShowLineNumbers = false
	ta.SetHeight(composeLines)
	ta.KeyMap.InsertNewline.SetEnabled(false) // Enter sends; Keys.Newline adds a line
	return ta
}

// Init subscribes to events and loads the first workspace.
func (m *Model) Init() tea.Cmd {
	if m.team == "" {
		return m.subscribe()
	}
	return tea.Batch(m.subscribe(), m.loadWorkspace(m.team))
}

// Update applies one message: a key, a size, a daemon result or event.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m, m.key(msg)
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.fixFocus()
		m.layoutCompose()
	case resultMsg:
		return m, m.result(msg)
	case NoteMsg:
		return m, m.note(msg)
	case DisconnectedMsg:
		m.gone = true
		m.setError("Lost the connection to the daemon. Quit and start omaslack tui again.")
	case ThemeMsg:
		m.theme = theme.Theme(msg)
		m.applyTheme()
	}
	return m, nil
}

func (m *Model) result(r resultMsg) tea.Cmd {
	if r.err != nil {
		m.failed(r)
		return nil
	}
	return tea.Batch(r.apply(m), m.afterChange())
}

// note applies one event. ui.focus (another client asked to show a
// conversation) is a request, not state: it switches the view.
func (m *Model) note(n NoteMsg) tea.Cmd {
	if n.Method == "ui.focus" {
		var f wire.FocusEvent
		if err := json.Unmarshal(n.Params, &f); err != nil {
			m.setError("ui.focus: " + err.Error())
			return nil
		}
		return m.focusConversation(f.Team, f.Conv)
	}
	if err := m.st.Apply(n.Method, n.Params, m.now()); err != nil {
		m.setError(err.Error())
		return nil
	}
	m.keepSelection()
	m.syncCompose()
	return m.afterChange()
}

// afterChange is what any new state may call for: marking the open
// conversation read and asking for names the view lacks.
func (m *Model) afterChange() tea.Cmd {
	return tea.Batch(m.markRead(), m.askNames())
}

func (m *Model) setError(s string) { m.status, m.statusIsError = s, true }
func (m *Model) setInfo(s string)  { m.status, m.statusIsError = s, false }

// openConv is the active workspace's open conversation, or "".
func (m *Model) openConv() string { return m.open[m.team] }

// messages are the open conversation's top-level messages.
func (m *Model) messages() []wire.Message {
	if c := m.openConv(); c != "" {
		return m.st.Messages(m.team, c)
	}
	return nil
}

// replies are the open thread's messages.
func (m *Model) replies() []wire.Message {
	if c := m.openConv(); c != "" && m.thread != "" {
		return m.st.Replies(m.team, c, m.thread)
	}
	return nil
}
