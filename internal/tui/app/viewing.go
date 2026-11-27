package app

import tea "charm.land/bubbletea/v2"

// viewing is what the TUI shows, as ui.viewing reports it to the daemon
// so it does not notify messages already on screen. The zero value shows
// nothing.
type viewing struct {
	target
	focused bool
}

// shown is what the TUI shows now: the open conversation and thread, and
// whether the terminal has focus. Terminals that do not report focus
// never send BlurMsg, so the TUI counts as focused while it runs.
func (m *Model) shown() viewing {
	conv := m.openConv()
	if conv == "" {
		return viewing{}
	}
	return viewing{target: target{m.team, conv, m.thread}, focused: !m.blurred}
}

// reportViewing tells the daemon when what the TUI shows changed. A
// daemon without ui.viewing is not asked again.
func (m *Model) reportViewing() tea.Cmd {
	v := m.shown()
	if v == m.viewing || m.gone {
		return nil
	}
	m.viewing = v
	params := map[string]any{}
	if v.conv != "" {
		params = v.params("focused", v.focused)
	}
	return optionalCall[any](m, "ui.viewing", params, nil)
}
