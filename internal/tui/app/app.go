// Package app is the TUI's bubbletea program (tasks 3.1, 3.2, 3.7): the
// panes (workspace rail, sidebar, messages, thread, compose, status bar),
// the NORMAL/INSERT/COMMAND modes and their keys, built-in help, and the
// view state, fed only by daemon method results and events.
package app

import (
	"context"
	"errors"

	"charm.land/bubbles/v2/key"
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
}

// New returns the root model (task 3.1).
func New(o Options) tea.Model { return nil }

// Run runs the program until the user quits or ctx ends (task 3.1).
func Run(ctx context.Context, o Options) error { return errTODO }

// Keys is the key map, documented in help (?) (task 3.2).
type Keys struct {
	Down, Up, Left, Right, NextPane key.Binding
	Insert, Back, Command, Help     key.Binding
	Finder, Quit                    key.Binding
}

var errTODO = errors.New("tui/app: not implemented yet (task 3.1)")
