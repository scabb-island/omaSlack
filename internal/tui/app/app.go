// Package app is the TUI's bubbletea program (tasks 3.1, 3.2, 3.7): the
// panes (workspace rail, sidebar, messages, thread, compose, status bar),
// the NORMAL/INSERT/COMMAND modes and their keys, built-in help, and the
// view state, fed only by daemon method results and events.
//
// Data flow: Init subscribes to the events once (events.subscribe) and
// loads the first workspace. Every daemon call runs as a tea.Cmd off the
// Update loop and comes back as a result that Update folds into State.
// Run forwards the daemon's notifications (NoteMsg), the end of the
// connection (DisconnectedMsg) and reloaded themes (ThemeMsg) into the
// program. The TUI never changes State on its own except to show at once
// what it just asked the daemon to do (a draft saved, a sent message's
// compose cleared); the daemon's answer or event settles it.
package app

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"gitlab.com/scabb-island/omaslack/internal/tui/client"
	"gitlab.com/scabb-island/omaslack/internal/tui/image"
	"gitlab.com/scabb-island/omaslack/internal/tui/theme"
)

// Options is what the app is built from. cli wires the real ones; tests
// pass client.Fake and a fixed theme.
type Options struct {
	Client client.Client
	Theme  theme.Theme
	Themes <-chan theme.Theme // reloaded themes (theme.Watch); nil for none
	Images image.Protocol
	Opener Opener // opens downloaded files; nil means xdg-open

	Now func() time.Time // the clock; nil means time.Now
	Loc *time.Location   // time zone for timestamps; nil means local
}

// New returns the root model (task 3.1).
func New(o Options) tea.Model { return newModel(o) }

// Run runs the program until the user quits or ctx ends (task 3.1).
func Run(ctx context.Context, o Options) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	p := tea.NewProgram(New(o), tea.WithContext(ctx))
	go forward(ctx, o, p.Send)
	_, err := p.Run()
	if ctx.Err() != nil {
		return nil
	}
	return err
}

// forward sends the daemon's events, the end of its connection and the
// reloaded themes to the program until ctx ends.
func forward(ctx context.Context, o Options, send func(tea.Msg)) {
	notes, done, themes := o.Client.Notifications(), o.Client.Done(), o.Themes
	for {
		select {
		case <-ctx.Done():
			return
		case n, ok := <-notes:
			if !ok {
				notes = nil
				continue
			}
			send(NoteMsg(n))
		case t, ok := <-themes:
			if !ok {
				themes = nil
				continue
			}
			send(ThemeMsg(t))
		case <-done:
			done = nil
			send(DisconnectedMsg{})
		}
	}
}
