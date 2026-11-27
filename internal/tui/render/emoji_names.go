package render

import (
	"maps"
	"slices"
	"strings"
	"sync"
)

// StandardEmojiNames lists the shortcodes StandardEmoji knows, sorted,
// without the skin tone modifiers: the emoji a picker can offer. The
// slice is the caller's.
func StandardEmojiNames() []string { return slices.Clone(standardNames()) }

var standardNames = sync.OnceValue(func() []string {
	names := slices.Sorted(maps.Keys(standardEmoji))
	return slices.DeleteFunc(names, func(n string) bool { return strings.HasPrefix(n, "skin-tone-") })
})
