package app

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"gitlab.com/scabb-island/omaslack/internal/tui/image"
	"gitlab.com/scabb-island/omaslack/internal/tui/render"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// Inline images (spec tui-client, "Images in the terminal"; task 3.5).
//
// The message panes draw an image under its "[image]" row (render marks
// those rows; render.ImageFileID finds them), loaded lazily: View notes
// the image rows it shows without a picture (wants), and a tick shortly
// after loads them, files.fetch for the local path, then image.Size and
// image.Render in a tea.Cmd. Pictures are cached by path, protocol and
// room (cols × imageMaxRows), so scrolling or redrawing never renders
// twice, and a resize renders again at the new width.
//
// Half-block pictures are text: View draws them in place. Kitty and sixel
// pictures are escape sequences the cell renderer cannot hold, so View
// leaves blank rows for them, marked on the first and the last with an
// invisible OSC 8 link (placeScheme), takes the marks out again and
// records where they were (placements). The tick then prints every
// picture with tea.Raw, wrapped in save cursor, a move to its top-left
// cell and restore cursor, so bubbletea's idea of the cursor stays right.
// The tick comes imageDelay after the Update that may have moved things,
// after bubbletea has flushed that frame, so the picture lands on top of
// it. A picture whose first or last row is scrolled out is not placed.
// Kitty images outlive the text, so the tick sends image.ClearKitty
// before placing them again whenever the placements change (scroll,
// resize, another conversation, an overlay, I). Sixel pixels belong to
// the cells, so bubbletea's redraws erase them where text changes, but
// cells it believes unchanged can keep stale pixels after a scroll: that
// is why inline images start off with sixel (I turns them on).

// Inline image limits.
const (
	imageMaxRows  = 12                    // the tallest picture, in rows
	imageMinCols  = 4                     // narrower panes show no pictures
	imageDelay    = 50 * time.Millisecond // from an Update to the tick that loads and places
	imageCacheMax = 64                    // pictures kept; past it the cache starts over
)

// imageTickMethod names the tick's resultMsg; it is not a daemon method.
const imageTickMethod = "images.tick"

// placeScheme marks the rows View leaves for a kitty or sixel picture.
const placeScheme = "omaslack-place:"

var placeMark = regexp.MustCompile("\x1b\\]8;;" + placeScheme + `(\d+)([be])` + "\a\x1b\\]8;;\a")

func mark(id int, edge byte) string {
	return ansi.SetHyperlink(placeScheme+strconv.Itoa(id)+string(edge)) + ansi.ResetHyperlink()
}

// fetched is a file's files.fetch answer; done is false while it is asked.
type fetched struct {
	path string
	err  error
	done bool
}

// imageKey is what a picture depends on.
type imageKey struct {
	path       string
	proto      image.Protocol
	cols, rows int // the room it may take
}

// picture is one rendered image; done is false while it renders.
type picture struct {
	id         int
	done       bool
	cols, rows int // the cells it covers
	out        string
	err        error
}

// imageWant is an image row View showed without its picture.
type imageWant struct {
	file fileKey
	cols int
}

// placement is a kitty or sixel picture on screen: where, and the
// sequence that draws it there.
type placement struct {
	row, col int
	seq      string
}

// imageCache holds what inline images need between frames.
type imageCache struct {
	inline  bool // I toggles it
	paths   map[fileKey]*fetched
	pics    map[imageKey]*picture
	byID    map[int]*picture
	nextID  int
	wants   []imageWant // noted by the last View
	placed  []placement // where the last View left room for pictures
	shown   []placement // what the terminal shows
	ticking bool        // a tick is on its way
}

// newImageCache starts with inline images on, except with sixel.
func newImageCache(p image.Protocol) imageCache {
	return imageCache{
		inline: p != image.Sixel,
		paths:  map[fileKey]*fetched{},
		pics:   map[imageKey]*picture{},
		byID:   map[int]*picture{},
	}
}

func (m *Model) inlineImages() bool { return m.media.cache.inline && m.canFiles() }

// toggleImages is the I key.
func (m *Model) toggleImages() tea.Cmd {
	m.media.cache.inline = !m.media.cache.inline
	if m.media.cache.inline {
		m.setInfo("Inline images on")
		return nil
	}
	m.setInfo("Inline images off")
	return nil
}

// withImages adds the pictures, or the room for them, under the image
// rows of lines, a message pane w columns wide.
func (m *Model) withImages(lines []string, w int) []string {
	if !m.inlineImages() {
		return lines
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, line)
		if !strings.Contains(line, render.ImageScheme) {
			continue
		}
		if id, ok := render.ImageFileID(line); ok {
			out = append(out, m.imageRows(fileKey{m.team, id}, indentOf(line), w)...)
		}
	}
	return out
}

func indentOf(line string) int {
	s := ansi.Strip(line)
	return len(s) - len(strings.TrimLeft(s, " "))
}

// imageRows are the rows under an image row: the half-block picture, the
// marked blank rows for a kitty or sixel one, or none while it loads.
func (m *Model) imageRows(fk fileKey, indent, w int) []string {
	cols := w - indent
	if cols < imageMinCols {
		return nil
	}
	p := m.picture(fk, cols)
	if p == nil || !p.done || p.err != nil {
		return nil
	}
	pad := strings.Repeat(" ", indent)
	if m.images == image.HalfBlock {
		rows := strings.Split(p.out, "\n")
		for i := range rows {
			rows[i] = pad + rows[i]
		}
		return rows
	}
	rows := slices.Repeat([]string{pad}, p.rows)
	rows[0] += mark(p.id, 'b')
	rows[len(rows)-1] += mark(p.id, 'e')
	return rows
}

// picture is fk's picture cols wide, or nil when it is not there yet,
// which View notes as a want. A file that cannot be fetched shows none.
func (m *Model) picture(fk fileKey, cols int) *picture {
	c := &m.media.cache
	f := c.paths[fk]
	if f != nil && f.done && f.err != nil {
		return nil
	}
	if f != nil && f.done {
		if p := c.pics[imageKey{f.path, m.images, cols, imageMaxRows}]; p != nil {
			return p
		}
	}
	c.wants = append(c.wants, imageWant{fk, cols})
	return nil
}

// frame is the screen View shows: with kitty or sixel, the room marks
// are taken out and their placements noted for the next tick.
func (m *Model) frame() string {
	c := &m.media.cache
	c.wants = c.wants[:0]
	s := m.screen()
	if m.images == image.HalfBlock {
		return s
	}
	c.placed, s = m.placements(s)
	return s
}

// spot is a room mark on screen.
type spot struct{ id, row, col int }

// placements finds the room marks of screen, and returns the pictures
// whose first and last rows both show, and screen without the marks.
func (m *Model) placements(screen string) ([]placement, string) {
	if !strings.Contains(screen, placeScheme) {
		return nil, screen
	}
	lines := strings.Split(screen, "\n")
	var begins []spot
	ends := map[spot]bool{}
	for row, line := range lines {
		for _, loc := range placeMark.FindAllStringSubmatchIndex(line, -1) {
			id, _ := strconv.Atoi(line[loc[2]:loc[3]])
			sp := spot{id, row, ansi.StringWidth(line[:loc[0]])}
			if line[loc[4]] == 'b' {
				begins = append(begins, sp)
			} else {
				ends[sp] = true
			}
		}
		lines[row] = placeMark.ReplaceAllString(line, "")
	}
	return m.placed(begins, ends), strings.Join(lines, "\n")
}

func (m *Model) placed(begins []spot, ends map[spot]bool) []placement {
	var out []placement
	for _, b := range begins {
		p := m.media.cache.byID[b.id]
		if p != nil && ends[spot{b.id, b.row + p.rows - 1, b.col}] {
			seq := ansi.SaveCursor + ansi.CursorPosition(b.col+1, b.row+1) + p.out + ansi.RestoreCursor
			out = append(out, placement{b.row, b.col, seq})
		}
	}
	return out
}

// imagesAfter runs after every Update: when the open conversation or
// thread has images, or pictures are on screen, it asks for a tick to
// load and place them once this Update's frame is drawn. A resize makes
// the terminal forget the pictures, so they are placed again.
func (m *Model) imagesAfter(msg tea.Msg) tea.Cmd {
	c := &m.media.cache
	if _, ok := msg.(tea.WindowSizeMsg); ok {
		c.shown = nil
	}
	if r, ok := msg.(resultMsg); (ok && r.method == imageTickMethod) || c.ticking || !m.imagesActive() {
		return nil
	}
	c.ticking = true
	return tea.Tick(imageDelay, func(time.Time) tea.Msg {
		return resultMsg{method: imageTickMethod, apply: (*Model).imageTick}
	})
}

// imagesActive says a tick may have work: pictures to clear, or images
// in the open conversation or thread.
func (m *Model) imagesActive() bool {
	c := &m.media.cache
	return len(c.shown) > 0 || len(c.wants) > 0 || m.inlineImages() && hasImages(append(m.messages(), m.replies()...))
}

func hasImages(msgs []wire.Message) bool {
	return slices.ContainsFunc(msgs, func(msg wire.Message) bool {
		return slices.ContainsFunc(msg.Files, func(f wire.File) bool { return strings.HasPrefix(f.MIMEType, "image/") })
	})
}

// imageTick loads what the last View wanted and places its pictures.
func (m *Model) imageTick() tea.Cmd {
	c := &m.media.cache
	c.ticking = false
	var cmds []tea.Cmd
	for _, w := range c.wants {
		cmds = append(cmds, m.loadImage(w))
	}
	return tea.Batch(append(cmds, m.place())...)
}

// loadImage fetches a wanted image's file, or renders its picture.
func (m *Model) loadImage(w imageWant) tea.Cmd {
	c := &m.media.cache
	f := c.paths[w.file]
	if f == nil {
		f = &fetched{}
		c.paths[w.file] = f
		return m.fetchFile(w.file, func(_ *Model, path string, err error) tea.Cmd {
			*f = fetched{path: path, err: err, done: true}
			return nil
		})
	}
	k := imageKey{f.path, m.images, w.cols, imageMaxRows}
	if !f.done || f.err != nil || c.pics[k] != nil {
		return nil
	}
	return m.renderImage(k)
}

// renderImage renders a picture off the Update loop.
func (m *Model) renderImage(k imageKey) tea.Cmd {
	c := &m.media.cache
	if len(c.pics) >= imageCacheMax {
		c.pics, c.byID = map[imageKey]*picture{}, map[int]*picture{}
	}
	c.nextID++
	p := &picture{id: c.nextID}
	c.pics[k], c.byID[p.id] = p, p
	return func() tea.Msg {
		cols, rows, err := image.Size(k.path, k.cols, k.rows)
		var out string
		if err == nil {
			out, err = image.Render(k.path, k.proto, k.cols, k.rows)
		}
		return resultMsg{method: "image.render", apply: func(*Model) tea.Cmd {
			*p = picture{id: p.id, done: true, cols: cols, rows: rows, out: out, err: err}
			return nil
		}}
	}
}

// place prints the kitty or sixel pictures where the last View left room
// for them, when that changed: kitty's old ones cleared first.
func (m *Model) place() tea.Cmd {
	c := &m.media.cache
	if m.images == image.HalfBlock || slices.Equal(c.placed, c.shown) {
		return nil
	}
	var b strings.Builder
	if m.images == image.Kitty {
		b.WriteString(image.ClearKitty)
	}
	for _, p := range c.placed {
		b.WriteString(p.seq)
	}
	c.shown = slices.Clone(c.placed)
	if b.Len() == 0 {
		return nil
	}
	return tea.Raw(b.String())
}
