package mrkdwn

import (
	"html"
	"net/url"
	"strings"
	"unicode"
)

// Names resolves the IDs mrkdwn refers to, for one workspace. "" means
// unknown: the HTML then shows the label the text carries, or the ID.
type Names interface {
	UserName(id string) string
	ConversationName(id string) string
}

// HTML renders mrkdwn as the subset of HTML Qt's Text.RichText draws, for
// the QML plugin (task 4.9):
//
//	*b* _i_ ~s~         <b>, <i>, <s>
//	`code`              <code>
//	```block```         <pre> (whitespace kept)
//	> quote, >>> rest   <blockquote>, consecutive quoted lines in one
//	line breaks         <br>
//	<url|label>         <a href="url">label</a>, for http, https and
//	                    mailto URLs only; any other target is its text
//	<@U1>               <a href="omaslack://user/<team>/U1">@name</a>
//	<#C1|name>          <a href="omaslack://conv/<team>/C1">#name</a>
//	<!here>             <b>@here</b> (also channel and everyone)
//	:emoji:             the Unicode character; custom ones stay :name:
//
// Every piece of text, names and URLs included, is escaped (&, <, >, "
// and '), so a message cannot inject markup. Nothing carries colors or
// fonts: QML styles the text from the theme. names may be nil.
func HTML(mrkdwn, team string, names Names) string {
	h := htmlVisitor{team: url.PathEscape(team), names: names}
	if names == nil {
		h.names = noNames{}
	}
	var b strings.Builder
	var group []string // the lines of the run being collected
	quoted := false
	flush := func() {
		b.WriteString(h.lines(group, quoted))
		group = nil
	}
	for _, blk := range Parse(mrkdwn, nil) {
		l, isLine := blk.(Line)
		if !isLine || l.Quoted != quoted {
			flush()
		}
		if !isLine {
			b.WriteString(h.pre(blk.(CodeBlock)))
			continue
		}
		quoted = l.Quoted
		group = append(group, Render(l.Inlines, h))
	}
	flush()
	return b.String()
}

type noNames struct{}

func (noNames) UserName(string) string         { return "" }
func (noNames) ConversationName(string) string { return "" }

// htmlVisitor renders inlines as escaped HTML for one workspace.
type htmlVisitor struct {
	team  string // path-escaped
	names Names
}

var _ Visitor = htmlVisitor{}

var esc = html.EscapeString

// lines joins a run of lines with <br>, in a <blockquote> when quoted.
func (htmlVisitor) lines(group []string, quoted bool) string {
	if len(group) == 0 {
		return ""
	}
	s := strings.Join(group, "<br>")
	if quoted {
		return "<blockquote>" + s + "</blockquote>"
	}
	return s
}

// pre renders a code block. Its text still carries Slack's entities, so
// they are decoded before everything is escaped again.
func (htmlVisitor) pre(c CodeBlock) string {
	return "<pre>" + esc(Clean(html.UnescapeString(c.Text))) + "</pre>"
}

func (htmlVisitor) Text(t Text) string { return esc(t.Text) }

func (htmlVisitor) Code(c Code) string { return "<code>" + esc(c.Text) + "</code>" }

var tags = map[Mark]string{Bold: "b", Italic: "i", Strike: "s"}

func (h htmlVisitor) Styled(s Styled) string {
	tag := tags[s.Mark]
	return "<" + tag + ">" + Render(s.Inlines, h) + "</" + tag + ">"
}

func (h htmlVisitor) User(u User) string {
	return anchor("omaslack://user/"+h.team+"/"+url.PathEscape(u.ID), "@"+userName(h.names, u))
}

func (h htmlVisitor) Channel(c Channel) string {
	return anchor("omaslack://conv/"+h.team+"/"+url.PathEscape(c.ID), "#"+channelName(h.names, c))
}

// Special renders <!…>: broadcasts and groups in bold, dates as their
// fallback text.
func (htmlVisitor) Special(s Special) string {
	if s.Loud() {
		return "<b>" + esc(s.Text()) + "</b>"
	}
	return esc(s.Text())
}

// Link is an anchor only for web and mail URLs: a message must not hand
// QML a javascript: or omaslack: link dressed as something else.
func (htmlVisitor) Link(l Link) string {
	text := l.Text()
	if !safeURL(l.URL) {
		return esc(text)
	}
	return anchor(l.URL, text)
}

func safeURL(s string) bool {
	lower := strings.ToLower(s)
	web := strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "mailto:")
	return web && !strings.ContainsFunc(s, unicode.IsSpace)
}

func (htmlVisitor) Shortcode(s Shortcode) string { return esc(s.Text) }

func anchor(href, text string) string {
	return `<a href="` + esc(href) + `">` + esc(text) + "</a>"
}
