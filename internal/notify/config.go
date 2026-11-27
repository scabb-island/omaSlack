package notify

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// Open is the client a click on a notification opens.
type Open uint8

const (
	OpenWindow Open = iota // the plugin's window, when the plugin is loaded
	OpenTUI                // the TUI, through omarchy-launch-or-focus-tui
)

// Config is the [notifications] table of config.toml.
type Config struct {
	Enabled  bool     // false: no notification at all
	Keywords []string // lower case; any of them in a message notifies
	Channels []Target // notify on every message
	Muted    []Target // never notify, whatever else matches
	Open     Open
}

// DefaultConfig is the configuration without a config.toml: enabled,
// nothing extra, clicks open the window.
func DefaultConfig() Config { return Config{Enabled: true, Open: OpenWindow} }

// Target names a conversation of the channels and muted lists: by ID or
// by name ("#general"), in any workspace or, with a "T…:" prefix, in one.
// Exactly one of ID and Name is set.
type Target struct {
	Team domain.TeamID // zero: any workspace
	ID   domain.ConversationID
	Name string // without the "#"
}

// Matches reports whether c of team is the conversation t names. Names
// compare without case, as Slack's do.
func (t Target) Matches(team domain.TeamID, c domain.Conversation) bool {
	if !t.Team.IsZero() && t.Team != team {
		return false
	}
	if !t.ID.IsZero() {
		return t.ID == c.ID
	}
	return t.Name != "" && strings.EqualFold(t.Name, c.Name)
}

// DefaultConfigPath is $XDG_CONFIG_HOME/omaslack/config.toml, read
// through getenv (os.Getenv); XDG_CONFIG_HOME defaults to ~/.config. ""
// when neither it nor HOME is set.
func DefaultConfigPath(getenv func(string) string) string {
	dir := getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home := getenv("HOME")
		if home == "" {
			return ""
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "omaslack", "config.toml")
}

// LoadConfig reads the config at path. A file that does not exist is
// DefaultConfig; one that cannot be read or parsed is an error naming it.
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the user's own config file
	if errors.Is(err, os.ErrNotExist) {
		return DefaultConfig(), nil
	}
	if err == nil {
		var cfg Config
		if cfg, err = ParseConfig(data); err == nil {
			return cfg, nil
		}
	}
	return Config{}, fmt.Errorf("notify: config %s: %w", path, err)
}

// file is config.toml as written; other tables are ignored.
type file struct {
	Notifications struct {
		Enabled  *bool
		Keywords []string
		Channels []string
		Muted    []string
		Open     string
	}
}

// ParseConfig reads config.toml's [notifications] table; what it leaves
// out keeps its default.
func ParseConfig(data []byte) (Config, error) {
	var f file
	if _, err := toml.Decode(string(data), &f); err != nil {
		return Config{}, err
	}
	n := f.Notifications
	cfg := DefaultConfig()
	if n.Enabled != nil {
		cfg.Enabled = *n.Enabled
	}
	cfg.Keywords = keywords(n.Keywords)
	var errs [3]error
	cfg.Channels, errs[0] = targets("channels", n.Channels)
	cfg.Muted, errs[1] = targets("muted", n.Muted)
	cfg.Open, errs[2] = parseOpen(n.Open)
	return cfg, errors.Join(errs[:]...)
}

func keywords(in []string) []string {
	var out []string
	for _, k := range in {
		if k = strings.ToLower(strings.TrimSpace(k)); k != "" {
			out = append(out, k)
		}
	}
	return out
}

func parseOpen(s string) (Open, error) {
	switch s {
	case "", "window":
		return OpenWindow, nil
	case "tui":
		return OpenTUI, nil
	}
	return 0, fmt.Errorf("open = %q: want \"window\" or \"tui\"", s)
}

func targets(key string, in []string) ([]Target, error) {
	var out []Target
	for _, s := range in {
		t, err := parseTarget(s)
		if err != nil {
			return nil, fmt.Errorf("%s: %q: %w", key, s, err)
		}
		out = append(out, t)
	}
	return out, nil
}

// parseTarget reads "[T…:]#name" or "[T…:]<conversation ID>".
func parseTarget(s string) (Target, error) {
	var t Target
	if teamPart, rest, ok := strings.Cut(s, ":"); ok {
		team, err := domain.ParseTeamID(teamPart)
		if err != nil {
			return Target{}, err
		}
		t.Team, s = team, rest
	}
	if name, ok := strings.CutPrefix(s, "#"); ok {
		if name == "" {
			return Target{}, errors.New("a channel name after #")
		}
		t.Name = name
		return t, nil
	}
	id, err := domain.ParseConversationID(s)
	if err != nil {
		return Target{}, fmt.Errorf("want #name or a conversation ID: %w", err)
	}
	t.ID = id
	return t, nil
}
