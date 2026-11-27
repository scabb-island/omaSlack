// Package render turns Slack messages into styled terminal text (spec
// messaging "Reading messages", "Mentions and autocomplete"; task 3.4).
// Everything here is a pure function of its inputs, so it is tested with
// golden files and never talks to the daemon.
//
// Output is styled text with true-color SGR sequences and OSC 8
// hyperlinks; the terminal program downsamples colors. Control characters
// in messages and names (an ESC above all) are dropped, so a message can
// never write its own escape sequences.
package render

import (
	"strings"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/tui/theme"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// Directory resolves what mrkdwn refers to by ID. The app fills it from
// users.info, conversations.list and emoji.list; an unknown ID renders as
// the ID itself (never an error).
type Directory interface {
	// UserName is the display name for a U…/W… ID (bots' B… IDs too), or
	// "" when unknown.
	UserName(id string) string
	// ConversationName is the name for a C…/G…/D… ID, or "" when unknown.
	ConversationName(id string) string
	// Emoji resolves a custom shortcode (no colons) to the plain text to
	// print, such as ":name:" (render styles it); standard shortcodes are
	// resolved by StandardEmoji first and never reach it. ok is false for
	// an unknown name, which is printed as ":name:" verbatim.
	Emoji(name string) (text string, ok bool)
	// Self is the signed-in user's ID, so mentions of them stand out.
	Self() string
}

// Options shape one render.
type Options struct {
	Theme theme.Theme
	Width int            // columns available; text wraps to it (0: no wrapping)
	Now   time.Time      // for "Today"/"Yesterday" day separators
	Loc   *time.Location // time zone for timestamps; nil means local
}

// Text renders one mrkdwn string: *bold*, _italic_, ~strike~, `code`,
// ```blocks``` (never wrapped: a long line is cut), > and >>> quotes,
// <url|label> links (OSC 8 hyperlinks when the target is a URL), <@U…>
// mentions (Theme.Mention for Self), <#C…|name> channels,
// <!here>/<!channel>/<!everyone>, <!subteam^…|@group>, <!date^…|fallback>,
// :emoji: (with ::skin-tone-N), and HTML entities. Malformed markup is
// printed as text. It wraps to o.Width in terminal cells, so CJK and emoji
// count two (task 3.4).
func Text(mrkdwn string, d Directory, o Options) string {
	return strings.Join(newPainter(o.Theme, d, false).blocks(mrkdwn, o.Width), "\n")
}

// Messages renders a conversation's messages, given oldest first, as
// terminal lines:
//
//	── Today ──────────────────────
//	alice  10:32                    header: name (B… authors get "[bot]"), HH:MM in o.Loc
//	  first message (edited)        body indented 2; "(edited)" after the last line
//	  next one within 5 minutes     grouped: same author, same day, < 5 minutes later
//	  [file] report.pdf · 1.2 MB · application/pdf
//	  [image] cat.png · 340.0 KB · image/png   tag links to ImageScheme+ID (see ImageFileID)
//	  👍 2  🎉 1                    reactions; the user's own in Theme.Accent
//	  ↳ thread                      a thread root (ThreadTS == TS)
//
// Day separators say Today, Yesterday or the date (with the year when it is
// not o.Now's). wire.Message carries no reply count, so a thread root gets
// "↳ thread", not "N replies". selected (an index into msgs, or -1) has
// every line filled to o.Width with Theme.Selection (task 3.4).
func Messages(msgs []wire.Message, selected int, d Directory, o Options) []string {
	return messageLines(msgs, selected, d, o)
}
