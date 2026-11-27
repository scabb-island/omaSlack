package render

import (
	"slices"
	"strings"
	"sync"
)

// StandardEmojiNames lists the standard shortcodes StandardEmoji knows,
// sorted, without the skin-tone modifiers: the names a picker or the
// compose autocomplete offers. The slice is the caller's.
func StandardEmojiNames() []string { return slices.Clone(standardNames()) }

var standardNames = sync.OnceValue(func() []string {
	names := make([]string, 0, len(standardEmoji))
	for name := range standardEmoji {
		if !strings.HasPrefix(name, "skin-tone-") {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
})
