package render

import (
	"slices"
	"strings"
	"testing"
)

func TestStandardEmojiNames(t *testing.T) {
	names := StandardEmojiNames()
	if !slices.IsSorted(names) || !slices.Contains(names, "+1") || !slices.Contains(names, "tada") {
		t.Fatalf("names %v", names[:min(len(names), 10)])
	}
	for _, n := range names {
		if strings.HasPrefix(n, "skin-tone-") {
			t.Fatalf("%s is a modifier, not an emoji to pick", n)
		}
		if _, ok := StandardEmoji(n); !ok {
			t.Fatalf("%s is listed but StandardEmoji does not know it", n)
		}
	}
	names[0] = "changed"
	if StandardEmojiNames()[0] == "changed" {
		t.Fatal("callers can change the shared list")
	}
}
