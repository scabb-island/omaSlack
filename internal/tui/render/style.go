package render

import (
	"image/color"

	"github.com/charmbracelet/x/ansi"
)

// style is the little render needs from a terminal style: colors, three
// attributes and a hyperlink. lipgloss.Style.Render also measures
// borders, margins and padding on every call, which made it most of the
// cost of a long conversation; this writes the SGR directly.
type style struct {
	fg, bg               color.Color
	bold, italic, strike bool
	link                 string
}

func (s style) Foreground(c color.Color) style { s.fg = c; return s }
func (s style) Background(c color.Color) style { s.bg = c; return s }
func (s style) Bold(v bool) style              { s.bold = v; return s }
func (s style) Italic(v bool) style            { s.italic = v; return s }
func (s style) Strikethrough(v bool) style     { s.strike = v; return s }
func (s style) Hyperlink(url string) style     { s.link = url; return s }

// Render wraps text in the style: one SGR, the text, a reset, and the
// hyperlink around them. Empty text renders as nothing.
func (s style) Render(text string) string {
	if text == "" {
		return ""
	}
	out := s.sgr().String() + text + sgrReset
	if s.link != "" {
		out = ansi.SetHyperlink(s.link) + out + ansi.ResetHyperlink()
	}
	return out
}

func (s style) sgr() ansi.Style {
	var a ansi.Style
	if s.bold {
		a = a.Bold()
	}
	if s.italic {
		a = a.Italic(true)
	}
	if s.strike {
		a = a.Strikethrough(true)
	}
	if s.fg != nil {
		a = a.ForegroundColor(s.fg)
	}
	if s.bg != nil {
		a = a.BackgroundColor(s.bg)
	}
	return a
}
