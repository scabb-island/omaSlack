package syncengine

import (
	"strings"
	"testing"
	"unicode/utf8"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

func TestSafeName(t *testing.T) {
	id, err := domain.ParseFileID("F0001")
	if err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("é", 150) // 300 bytes; non-ASCII on purpose
	for name, want := range map[string]string{
		"report.pdf":          "report.pdf",
		"../../etc/passwd":    ".._.._etc_passwd",
		`..\..\x`:             ".._.._x",
		"..":                  "F0001",
		".":                   "F0001",
		"":                    "F0001",
		"tab\there\x7f":       "tab_here_",
		"bad\xffutf8":         "bad_utf8",
		"café 🚀.png":          "café 🚀.png", // non-ASCII on purpose
		long:                  strings.Repeat("é", maxNameBytes/2),
		long[:maxNameBytes+1]: strings.Repeat("é", maxNameBytes/2),
	} {
		got := safeName(domain.File{ID: id, Name: name})
		if got != want {
			t.Errorf("safeName(%q) = %q, want %q", name, got, want)
		}
		if len(got) > maxNameBytes || !utf8.ValidString(got) || strings.ContainsAny(got, "/\x00") {
			t.Errorf("safeName(%q) = %q is not a safe path component", name, got)
		}
	}
}
