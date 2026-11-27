package render

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// Goldens: each case writes testdata/golden/<case>.ansi (exact output,
// escapes included) and <case>.txt (escapes stripped, for review). Run
// `go test ./internal/tui/render -update` to rewrite them. The inputs use
// non-ASCII text on purpose.
var update = flag.Bool("update", false, "rewrite the golden files in testdata/golden")

func TestGoldenText(t *testing.T) {
	var fixture struct {
		Cases map[string]struct {
			Width int    `json:"width"`
			Text  string `json:"text"`
		} `json:"cases"`
	}
	readJSON(t, "testdata/text.json", &fixture)
	for _, name := range sortedKeys(fixture.Cases) {
		c := fixture.Cases[name]
		t.Run(name, func(t *testing.T) {
			golden(t, "text-"+name, Text(c.Text, testDir(), testOptions(c.Width)))
		})
	}
}

func TestGoldenMessages(t *testing.T) {
	files, err := filepath.Glob("testdata/messages/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no message fixtures: %v", err)
	}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".json")
		t.Run(name, func(t *testing.T) {
			var fixture struct {
				Width    int            `json:"width"`
				Selected int            `json:"selected"`
				Messages []wire.Message `json:"messages"`
			}
			readJSON(t, f, &fixture)
			lines := Messages(fixture.Messages, fixture.Selected, testDir(), testOptions(fixture.Width))
			golden(t, "messages-"+name, strings.Join(lines, "\n"))
		})
	}
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	for ext, content := range map[string]string{".ansi": got + "\n", ".txt": strip(got) + "\n"} {
		path := filepath.Join("testdata", "golden", name+ext)
		if *update {
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%v (run with -update to create it)", err)
		}
		if string(want) != content {
			t.Errorf("%s differs from the output:\n got:\n%s\nwant:\n%s", path, content, want)
		}
	}
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
