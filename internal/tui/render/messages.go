package render

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// groupWindow is how close two messages from one author must be to share
// a header.
const groupWindow = 5 * time.Minute

// indent is how far a message's body sits under its header.
const indent = 2

// ImageScheme is the OSC 8 hyperlink scheme on the "[image]" tag of an
// image file row: "omaslack-image:<file id>". The app finds those lines
// with ImageFileID and can draw the image below them (task 3.5). A
// terminal that follows the link does nothing useful, but nothing harmful.
const ImageScheme = "omaslack-image:"

var imageLink = regexp.MustCompile("\x1b\\]8;[^;\x07\x1b]*;" + ImageScheme + "([A-Za-z0-9]+)(?:\x07|\x1b\\\\)")

// ImageFileID reports the file ID of an image row produced by Messages.
func ImageFileID(line string) (string, bool) {
	m := imageLink.FindStringSubmatch(line)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// messageLayout is what one message needs to know about its neighbors.
type messageLayout struct {
	t        time.Time
	newDay   bool
	grouped  bool
	selected bool
}

func layouts(msgs []wire.Message, selected int, loc *time.Location) []messageLayout {
	out := make([]messageLayout, len(msgs))
	for i, m := range msgs {
		l := messageLayout{t: tsTime(m.TS).In(loc), newDay: true, selected: i == selected}
		if i > 0 {
			prev := out[i-1]
			l.newDay = !sameDay(l.t, prev.t)
			gap := l.t.Sub(prev.t)
			l.grouped = !l.newDay && m.Author == msgs[i-1].Author && gap >= 0 && gap < groupWindow
		}
		out[i] = l
	}
	return out
}

func messageLines(msgs []wire.Message, selected int, d Directory, o Options) []string {
	loc := o.Loc
	if loc == nil {
		loc = time.Local
	}
	plain := newPainter(o.Theme, d, false)
	now := o.Now.In(loc)
	var out []string
	for i, l := range layouts(msgs, selected, loc) {
		if l.newDay {
			out = append(out, plain.separator(dayLabel(l.t, now), o.Width))
		}
		out = append(out, newPainter(o.Theme, d, l.selected).message(msgs[i], l, o.Width)...)
	}
	return out
}

// tsTime reads a Slack ts ("1700000000.000100"); a broken one is the epoch.
func tsTime(ts string) time.Time {
	sec, _, _ := strings.Cut(ts, ".")
	n, err := strconv.ParseInt(sec, 10, 64)
	if err != nil {
		n = 0
	}
	return time.Unix(n, 0)
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func dayLabel(t, now time.Time) string {
	switch {
	case sameDay(t, now):
		return "Today"
	case sameDay(t, now.AddDate(0, 0, -1)):
		return "Yesterday"
	case t.Year() == now.Year():
		return t.Format("Monday, January 2")
	}
	return t.Format("Monday, January 2, 2006")
}

func (p painter) muted() style  { return p.base.Foreground(p.t.Muted) }
func (p painter) accent() style { return p.base.Foreground(p.t.Accent) }

// space is n blank cells, in the selection background when there is one.
func (p painter) space(n int) string {
	s := strings.Repeat(" ", max(n, 0))
	if p.filled {
		return p.base.Render(s)
	}
	return s
}

func (p painter) separator(label string, width int) string {
	head := "── " + label + " "
	fill := 2
	if width > 0 {
		fill = width - ansi.StringWidth(head)
	}
	line := head + strings.Repeat("─", max(fill, 0))
	if width > 0 {
		line = ansi.Truncate(line, width, "")
	}
	return p.muted().Render(line)
}

// message renders one message: its header unless grouped, then the
// indented body, files, reactions and thread mark. A selected message is
// filled to width.
func (p painter) message(m wire.Message, l messageLayout, width int) []string {
	var lines []string
	if !l.grouped {
		lines = wrap(p.header(m, l.t), width)
	}
	w := 0
	if width > 0 {
		w = max(width-indent, 1)
	}
	for _, b := range p.body(m, w) {
		lines = append(lines, p.space(indent)+b)
	}
	if p.filled && width > 0 {
		for i, s := range lines {
			lines[i] = s + p.space(width-ansi.StringWidth(s))
		}
	}
	return lines
}

func (p painter) header(m wire.Message, t time.Time) string {
	name := firstOf(oneLine(p.d.UserName(m.Author)), oneLine(m.Author), "unknown")
	h := p.base.Bold(true).Render(name)
	if strings.HasPrefix(m.Author, "B") {
		h += p.space(1) + p.muted().Render("[bot]")
	}
	return h + p.space(2) + p.muted().Render(t.Format("15:04"))
}

func (p painter) body(m wire.Message, width int) []string {
	var lines []string
	if m.Text != "" {
		lines = p.blocks(m.Text, width)
	}
	lines = p.edited(lines, m.Edited != "", width)
	for _, f := range m.Files {
		lines = append(lines, cut(p.file(f), width))
	}
	lines = append(lines, p.reactions(m.Reactions, width)...)
	if m.ThreadTS != "" && m.ThreadTS == m.TS {
		lines = append(lines, p.accent().Render(threadMark(m.ReplyCount)))
	}
	return lines
}

// threadMark is what a thread root shows under it: how many replies it
// has, or just that it is a thread while the count is unknown.
func threadMark(replies int) string {
	switch replies {
	case 0:
		return "↳ thread"
	case 1:
		return "↳ 1 reply"
	default:
		return fmt.Sprintf("↳ %d replies", replies)
	}
}

// edited puts "(edited)" after the last line, or under it when it would
// not fit.
func (p painter) edited(lines []string, edited bool, width int) []string {
	if !edited {
		return lines
	}
	mark := p.muted().Render("(edited)")
	last := len(lines) - 1
	if last >= 0 && (width <= 0 || ansi.StringWidth(lines[last])+1+ansi.StringWidth(mark) <= width) {
		lines[last] += p.space(1) + mark
		return lines
	}
	return append(lines, mark)
}

// cut keeps s on one line, ending in "…" when it does not fit.
func cut(s string, width int) string {
	if width <= 0 {
		return s
	}
	return ansi.Truncate(s, width, "…")
}

// file is one row, cut to one line: "[file] name · size · type". An
// image's tag carries ImageScheme so the app can draw it inline.
func (p painter) file(f wire.File) string {
	tag := p.muted().Render("[file]")
	if strings.HasPrefix(f.MIMEType, "image/") && isFileID(f.ID) {
		tag = p.muted().Hyperlink(ImageScheme + f.ID).Render("[image]")
	}
	row := tag + p.space(1) + p.accent().Render(firstOf(oneLine(f.Name), oneLine(f.Title), oneLine(f.ID)))
	row += p.muted().Render(" · " + size(f.Size))
	if f.MIMEType != "" {
		row += p.muted().Render(" · " + oneLine(f.MIMEType))
	}
	return row
}

// isFileID accepts Slack's IDs (F0123ABC): anything else stays out of the
// escape sequence.
func isFileID(id string) bool {
	isAlnum := func(r rune) bool { return r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' }
	return id != "" && strings.IndexFunc(id, func(r rune) bool { return !isAlnum(r) }) < 0
}

func size(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	v := float64(n) / 1024
	for _, unit := range []string{"KB", "MB", "GB"} {
		if v < 1024 || unit == "GB" {
			return fmt.Sprintf("%.1f %s", v, unit)
		}
		v /= 1024
	}
	return ""
}

// reactions is one wrapped row of "emoji count", the user's own in
// Theme.Accent.
func (p painter) reactions(rs []wire.Reaction, width int) []string {
	if len(rs) == 0 {
		return nil
	}
	items := make([]string, len(rs))
	for i, r := range rs {
		e, ok := emojiText(r.Emoji, p.d)
		if !ok {
			e = ":" + oneLine(r.Emoji) + ":"
		}
		style := p.muted()
		if containsString(r.Users, p.d.Self()) {
			style = p.accent().Bold(true)
		}
		items[i] = style.Render(e + " " + strconv.Itoa(len(r.Users)))
	}
	return wrap(strings.Join(items, p.space(2)), width)
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
