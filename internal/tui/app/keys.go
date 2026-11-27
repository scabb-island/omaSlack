package app

import "charm.land/bubbles/v2/key"

// Keys is the key map, documented in help (?) (task 3.2). The keys follow
// slk where it has one (j/k, h/l, i, Esc, :, 1–9, Ctrl+k, ?), so slk users
// have nothing to relearn.
type Keys struct {
	Down, Up, Left, Right, NextPane key.Binding
	Insert, Back, Command, Help     key.Binding
	Finder, Quit, Renew             key.Binding

	PrevPane, Top, Bottom               key.Binding // NORMAL: more movement
	Open, Workspace                     key.Binding // NORMAL: open a conversation or thread; 1–9
	Sidebar, Narrower, Wider            key.Binding // NORMAL: show, hide and resize the sidebar
	Send, Newline                       key.Binding // INSERT
	OpenFile, Images, Attach            key.Binding // files: o and I in NORMAL, Ctrl+o in INSERT
	Search, React                       key.Binding // NORMAL: search messages; react to the selected message
	ListDown, ListUp, Pick              key.Binding // lists: finder, search results, reactions
	Edit, Delete, Reply                 key.Binding // NORMAL: on the selected message
	Suggest, Accept, NextItem, PrevItem key.Binding // INSERT: the autocomplete popup
}

// DefaultKeys is the key map omaslack ships with.
func DefaultKeys() Keys {
	b := func(keys []string, help, desc string) key.Binding {
		return key.NewBinding(key.WithKeys(keys...), key.WithHelp(help, desc))
	}
	return Keys{
		Down:      b([]string{"j", "down"}, "j/↓", "move down (newer)"),
		Up:        b([]string{"k", "up"}, "k/↑", "move up (older; loads history at the top)"),
		Top:       b([]string{"g", "home"}, "g", "go to the top"),
		Bottom:    b([]string{"G", "end"}, "G", "go to the newest"),
		Left:      b([]string{"h", "left"}, "h/←", "pane to the left"),
		Right:     b([]string{"l", "right"}, "l/→", "pane to the right"),
		NextPane:  b([]string{"tab"}, "Tab", "next pane"),
		PrevPane:  b([]string{"shift+tab"}, "S-Tab", "previous pane"),
		Open:      b([]string{"enter"}, "Enter", "open conversation / thread of message"),
		Workspace: b([]string{"1", "2", "3", "4", "5", "6", "7", "8", "9"}, "1–9", "switch workspace"),
		Finder:    b([]string{"ctrl+k", "ctrl+t"}, "C-k/C-t", "find a conversation in any workspace"),
		Sidebar:   b([]string{"ctrl+b"}, "C-b", "show or hide the sidebar"),
		Narrower:  b([]string{"<"}, "<", "narrower sidebar"),
		Wider:     b([]string{">"}, ">", "wider sidebar"),
		Insert:    b([]string{"i"}, "i", "write a message (INSERT)"),
		Command:   b([]string{":"}, ":", "command line (COMMAND)"),
		Back:      b([]string{"esc"}, "Esc", "back to NORMAL / close the thread"),
		Help:      b([]string{"?"}, "?", "this help"),
		Quit:      b([]string{"q", "ctrl+c"}, "q", "quit"),
		Renew:     b([]string{"ctrl+r"}, "C-r", "renew an expired desktop session"),
		Send:      b([]string{"enter"}, "Enter", "send"),
		Newline:   b([]string{"alt+enter", "ctrl+j"}, "A-Enter/C-j", "new line"),
		OpenFile:  b([]string{"o"}, "o", "open the message's file (xdg-open)"),
		Images:    b([]string{"I"}, "I", "show or hide inline images"),
		Attach:    b([]string{"ctrl+o"}, "C-o", "attach a file (:attach)"),
		Search:    b([]string{"/"}, "/", "search messages in this workspace"),
		React:     b([]string{"r"}, "r", "react to the selected message"),
		ListDown:  b([]string{"down", "ctrl+n", "ctrl+j"}, "↓/C-n", "next (j too in search results)"),
		ListUp:    b([]string{"up", "ctrl+p", "ctrl+k"}, "↑/C-p", "previous (k too in search results)"),
		Pick:      b([]string{"enter"}, "Enter", "pick"),
		Edit:      b([]string{"e"}, "e", "edit your message"),
		Delete:    b([]string{"D"}, "D", "delete your message (asks y/n)"),
		Reply:     b([]string{"R"}, "R", "reply in the message's thread"),
		Suggest:   b([]string{"@", "#", ":"}, "@ # :", "suggest people, channels, emoji"),
		Accept:    b([]string{"tab", "enter"}, "Tab/Enter", "pick the suggestion (Esc closes)"),
		NextItem:  b([]string{"down", "ctrl+n"}, "↓/C-n", "next suggestion"),
		PrevItem:  b([]string{"up", "ctrl+p"}, "↑/C-p", "previous suggestion"),
	}
}

// helpGroup is one titled block of the help overlay.
type helpGroup struct {
	title string
	keys  []key.Binding
}

// groups is the key map as help shows it, so help cannot drift from the
// keys that work.
func (k Keys) groups() []helpGroup {
	return []helpGroup{
		{"Move (NORMAL)", []key.Binding{k.Down, k.Up, k.Top, k.Bottom, k.Left, k.Right, k.NextPane, k.PrevPane}},
		{"Conversations", []key.Binding{k.Open, k.Workspace, k.Finder, k.Sidebar, k.Narrower, k.Wider, k.Renew}},
		{"Modes", []key.Binding{k.Insert, k.Command, k.Back, k.Help, k.Quit}},
		{"Compose (INSERT)", []key.Binding{k.Send, k.Newline, k.Back}},
		{"Files", []key.Binding{k.OpenFile, k.Images, k.Attach}},
		{"Find, search, react", []key.Binding{k.Search, k.React, k.ListDown, k.ListUp, k.Pick}},
		{"Messages (NORMAL)", []key.Binding{k.Edit, k.Delete, k.Reply}},
		{"Autocomplete (INSERT)", []key.Binding{k.Suggest, k.Accept, k.NextItem, k.PrevItem}},
	}
}

// commandHelp documents the COMMAND mode commands.
var commandHelp = [][2]string{
	{":q", "quit"},
	{":open <name>", "open a conversation by name (#general, @ana)"},
	{":help", "this help"},
	{":attach <path>", "send a file here; any text after it is the caption"},
	{":search <query>", "search messages in this workspace"},
	{":react <emoji>", "react to the selected message (:react +1)"},
	{":away / :back", "set your presence to away / automatic"},
	{":dnd <length>", "do not disturb: 30m, 1h, 2h, tomorrow"},
	{":dnd off", "end do not disturb"},
}
