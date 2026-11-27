package image

import "testing"

func TestProtocolString(t *testing.T) {
	if Kitty.String() != "kitty" || Sixel.String() != "sixel" || HalfBlock.String() != "halfblock" || Protocol(9).String() != "unknown" {
		t.Fatal("Protocol.String names")
	}
}

func TestDetect(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want Protocol
	}{
		{"alacritty", map[string]string{"TERM": "alacritty"}, HalfBlock},
		{"empty environment", nil, HalfBlock},
		{"kitty window id", map[string]string{"KITTY_WINDOW_ID": "1", "TERM": "xterm-256color"}, Kitty},
		{"kitty TERM", map[string]string{"TERM": "xterm-kitty"}, Kitty},
		{"ghostty TERM_PROGRAM", map[string]string{"TERM_PROGRAM": "ghostty", "TERM": "xterm-256color"}, Kitty},
		{"ghostty TERM", map[string]string{"TERM": "xterm-ghostty"}, Kitty},
		{"wezterm", map[string]string{"TERM_PROGRAM": "WezTerm"}, Kitty},
		{"foot", map[string]string{"TERM": "foot"}, Sixel},
		{"foot direct", map[string]string{"TERM": "foot-direct"}, Sixel},
		{"mlterm", map[string]string{"TERM": "mlterm"}, Sixel},
		{"contour", map[string]string{"TERM_PROGRAM": "contour"}, Sixel},
		{"iterm2", map[string]string{"TERM_PROGRAM": "iTerm.app"}, Sixel},
		{"tmux inside kitty", map[string]string{"TMUX": "/tmp/tmux-1000/default,1,0", "KITTY_WINDOW_ID": "1", "TERM": "tmux-256color"}, HalfBlock},
		{"screen inside foot", map[string]string{"TERM": "screen-256color"}, HalfBlock},
		{"zellij inside kitty", map[string]string{"ZELLIJ": "0", "TERM": "xterm-kitty"}, HalfBlock},
		{"override to sixel", map[string]string{"OMASLACK_IMAGES": "sixel", "TERM": "alacritty"}, Sixel},
		{"override to halfblock", map[string]string{"OMASLACK_IMAGES": "halfblock", "TERM": "xterm-kitty"}, HalfBlock},
		{"override to kitty inside tmux", map[string]string{"OMASLACK_IMAGES": "kitty", "TMUX": "x"}, Kitty},
		{"unknown override is ignored", map[string]string{"OMASLACK_IMAGES": "ascii", "TERM": "foot"}, Sixel},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Detect(func(k string) string { return c.env[k] }); got != c.want {
				t.Fatalf("Detect = %v, want %v", got, c.want)
			}
		})
	}
}
