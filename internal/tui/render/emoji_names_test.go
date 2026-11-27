package render

import (
	"slices"
	"testing"
)

func TestStandardEmojiNames(t *testing.T) {
	names := StandardEmojiNames()
	if !slices.IsSorted(names) {
		t.Error("names are not sorted")
	}
	for _, want := range []string{"tada", "+1", "smile"} {
		if !slices.Contains(names, want) {
			t.Errorf("%q missing", want)
		}
	}
	if slices.Contains(names, "skin-tone-2") {
		t.Error("skin tones are modifiers, not emoji to pick")
	}
	names[0] = "changed"
	if StandardEmojiNames()[0] == "changed" {
		t.Error("callers can change the shared list")
	}
}
