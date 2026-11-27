package notify

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

func TestParseConfig(t *testing.T) {
	cfg, err := ParseConfig([]byte(`
[notifications]
enabled = false
keywords = ["Deploy", "  ", "on call"]
channels = ["#incidents", "T01:C123"]
muted = ["E09:#random", "D042"]
open = "tui"

[theme]
ignored = "other tables are not the notifier's"
`))
	if err != nil {
		t.Fatal(err)
	}
	want := Config{
		Enabled:  false,
		Keywords: []string{"deploy", "on call"},
		Channels: []Target{{Name: "incidents"}, {Team: team(t, "T01"), ID: conv(t, "C123")}},
		Muted:    []Target{{Team: team(t, "E09"), Name: "random"}, {ID: conv(t, "D042")}},
		Open:     OpenTUI,
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("got %+v\nwant %+v", cfg, want)
	}
}

func TestParseConfigDefaults(t *testing.T) {
	for _, in := range []string{"", "[notifications]\n", "[other]\nx = 1\n"} {
		cfg, err := ParseConfig([]byte(in))
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if !reflect.DeepEqual(cfg, DefaultConfig()) {
			t.Errorf("%q: got %+v, want the defaults", in, cfg)
		}
	}
	if d := DefaultConfig(); !d.Enabled || d.Open != OpenWindow {
		t.Errorf("defaults: %+v", d)
	}
}

func TestParseConfigRejects(t *testing.T) {
	for _, in := range []string{
		"[notifications\n",
		"[notifications]\nenabled = \"yes\"\n",
		"[notifications]\nopen = \"browser\"\n",
		"[notifications]\nchannels = [\"general\"]\n",
		"[notifications]\nmuted = [\"#\"]\n",
		"[notifications]\nmuted = [\"X1:#a\"]\n",
		"[notifications]\nchannels = [\"T01:\"]\n",
	} {
		if _, err := ParseConfig([]byte(in)); err == nil {
			t.Errorf("%q: no error", in)
		}
	}
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadConfig(filepath.Join(dir, "missing.toml"))
	if err != nil || !reflect.DeepEqual(cfg, DefaultConfig()) {
		t.Errorf("a missing file is the defaults: %+v, %v", cfg, err)
	}
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[notifications]\nopen = \"tui\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if cfg, err := LoadConfig(path); err != nil || cfg.Open != OpenTUI {
		t.Errorf("got %+v, %v", cfg, err)
	}
	if err := os.WriteFile(path, []byte("[notifications]\nopen = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("a bad file names itself: %v", err)
	}
	if _, err := LoadConfig(dir); err == nil || errors.Is(err, os.ErrNotExist) {
		t.Errorf("a directory is not a config: %v", err)
	}
}

func TestDefaultConfigPath(t *testing.T) {
	env := map[string]string{"XDG_CONFIG_HOME": "/x", "HOME": "/h"}
	getenv := func(k string) string { return env[k] }
	if got := DefaultConfigPath(getenv); got != "/x/omaslack/config.toml" {
		t.Errorf("got %q", got)
	}
	delete(env, "XDG_CONFIG_HOME")
	if got := DefaultConfigPath(getenv); got != "/h/.config/omaslack/config.toml" {
		t.Errorf("got %q", got)
	}
	delete(env, "HOME")
	if got := DefaultConfigPath(getenv); got != "" {
		t.Errorf("got %q, want none", got)
	}
}

func TestTargetMatches(t *testing.T) {
	general := domain.Conversation{ID: conv(t, "C01"), Kind: domain.Channel, Name: "general"}
	t1, t2 := team(t, "T01"), team(t, "T02")
	cases := []struct {
		target Target
		team   domain.TeamID
		want   bool
	}{
		{Target{Name: "general"}, t1, true},
		{Target{Name: "General"}, t1, true},
		{Target{Name: "random"}, t1, false},
		{Target{ID: conv(t, "C01")}, t2, true},
		{Target{ID: conv(t, "C02")}, t1, false},
		{Target{Team: t1, Name: "general"}, t1, true},
		{Target{Team: t1, Name: "general"}, t2, false},
	}
	for _, c := range cases {
		if got := c.target.Matches(c.team, general); got != c.want {
			t.Errorf("%+v in %v: got %v", c.target, c.team, got)
		}
	}
	dm := domain.Conversation{ID: conv(t, "D01"), Kind: domain.DirectMessage}
	if (Target{Name: ""}).Matches(t1, dm) {
		t.Error("an unnamed conversation matches no name")
	}
}

func team(t *testing.T, s string) domain.TeamID {
	t.Helper()
	id, err := domain.ParseTeamID(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func conv(t *testing.T, s string) domain.ConversationID {
	t.Helper()
	id, err := domain.ParseConversationID(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
