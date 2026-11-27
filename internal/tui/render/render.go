// Package render turns Slack messages into styled terminal text (spec
// messaging "Reading messages", "Mentions and autocomplete"; task 3.4).
// Everything here is a pure function of its inputs, so it is tested with
// golden files and never talks to the daemon.
package render

import (
	"time"

	"gitlab.com/scabb-island/omaslack/internal/tui/theme"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// Directory resolves what mrkdwn refers to by ID. The app fills it from
// users.info, conversations.list and emoji.list; an unknown ID renders as
// the ID itself (never an error).
type Directory interface {
	// UserName is the display name for a U…/W… ID, or "" when unknown.
	UserName(id string) string
	// ConversationName is the name for a C…/G…/D… ID, or "" when unknown.
	ConversationName(id string) string
	// Emoji resolves a shortcode (no colons) to what to print: a Unicode
	// emoji for standard ones, or ":name:" styled for custom ones. ok is
	// false for an unknown name, which is printed as ":name:" verbatim.
	Emoji(name string) (text string, ok bool)
	// Self is the signed-in user's ID, so mentions of them stand out.
	Self() string
}

// Options shape one render.
type Options struct {
	Theme theme.Theme
	Width int            // columns available; text wraps to it
	Now   time.Time      // for "Today"/"Yesterday" day separators
	Loc   *time.Location // time zone for timestamps; nil means local
}

// Text renders one mrkdwn string: *bold*, _italic_, ~strike~, `code`,
// ```blocks```, > quotes, <url|label> links, <@U…> mentions, <#C…|name>
// channels, <!here>/<!channel>/<!everyone>, :emoji:, and &amp; &lt; &gt;
// entities. It wraps to o.Width (task 3.4).
func Text(mrkdwn string, d Directory, o Options) string { return mrkdwn }

// Messages renders a conversation's messages, given oldest first, as
// terminal lines: day separators, author and time headers (consecutive
// messages from one author within 5 minutes are grouped), the text, an
// "(edited)" mark, file rows, reactions, and a "N replies" line for thread
// roots when known. selected (an index into msgs, or -1) is highlighted
// (task 3.4).
func Messages(msgs []wire.Message, selected int, d Directory, o Options) []string { return nil }
