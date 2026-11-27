package app

import (
	goimage "image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"gitlab.com/scabb-island/omaslack/internal/tui/client"
	"gitlab.com/scabb-island/omaslack/internal/tui/image"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// writePNG writes a 160×120 PNG of four colored quadrants: 16×6 cells.
func writePNG(t *testing.T) string {
	t.Helper()
	img := goimage.NewRGBA(goimage.Rect(0, 0, 160, 120))
	quadrants := []color.RGBA{{255, 0, 0, 255}, {0, 255, 0, 255}, {0, 0, 255, 255}, {255, 255, 0, 255}}
	for y := range 120 {
		for x := range 160 {
			img.SetRGBA(x, y, quadrants[2*(y/60)+x/80])
		}
	}
	path := filepath.Join(t.TempDir(), "shot.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	return path
}

func shot() wire.File {
	return wire.File{ID: "F9", Name: "shot.png", MIMEType: "image/png", Size: 1234}
}

// imageDaemon serves #general with an image on its newest message, which
// files.fetch answers with path.
func imageDaemon(t *testing.T, path string) *client.Fake {
	t.Helper()
	return filesDaemon(t, []wire.File{shot()}, map[string]client.Handler{
		"files.fetch": client.Result(map[string]string{"localPath": path}),
	})
}

// openImages opens #general with protocol p.
func openImages(t *testing.T, f *client.Fake, p image.Protocol) *harness {
	t.Helper()
	h := newHarness(t, f, 120, 40)
	h.m.images, h.m.media.cache = p, newImageCache(p)
	h.keys(":")
	h.typeText("open general")
	h.keys("enter")
	return h
}

// settle draws and ticks until the pictures are loaded, as the program
// would: View notes what it wants, the tick loads it. It answers what the
// ticks printed raw.
func (h *harness) settle() string {
	h.t.Helper()
	var raw string
	for range 3 {
		h.m.View()
		raw += h.tick()
	}
	return raw
}

// tick sends the image tick, delivers what it answers and returns what
// it printed raw.
func (h *harness) tick() string {
	h.t.Helper()
	_, cmd := h.m.Update(resultMsg{method: imageTickMethod, apply: (*Model).imageTick})
	var raw strings.Builder
	for _, msg := range h.exec(cmd) {
		if r, ok := msg.(tea.RawMsg); ok {
			raw.WriteString(r.Msg.(string))
			continue
		}
		h.send(msg)
	}
	return raw.String()
}

func TestGoldenInlineHalfBlockImage(t *testing.T) {
	h := openImages(t, imageDaemon(t, writePNG(t)), image.HalfBlock)
	if raw := h.settle(); raw != "" {
		t.Fatalf("half-block printed raw %q", raw)
	}
	if h.count("files.fetch") != 1 {
		t.Fatalf("calls %v", h.calls())
	}
	if view := h.m.View().Content; !strings.Contains(view, "\x1b[38;2;255;0;0m") {
		t.Fatal("the picture's colors are missing")
	}
	h.golden("image_halfblock_120x40")
}

func TestInlineImagesToggle(t *testing.T) {
	h := openImages(t, imageDaemon(t, writePNG(t)), image.HalfBlock)
	h.settle()
	h.keys("I")
	if strings.Contains(h.screen(), "▀") {
		t.Fatalf("image still shown:\n%s", h.screen())
	}
	h.keys("I")
	if !strings.Contains(h.screen(), "▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀") {
		t.Fatalf("image not shown again:\n%s", h.screen())
	}
	if h.count("files.fetch") != 1 {
		t.Fatalf("fetched again: %v", h.calls())
	}
}

func TestInlineImagesNeedTheFilesCapability(t *testing.T) {
	msgs := generalMessages()
	msgs[0].Files = []wire.File{shot()}
	h := newHarness(t, daemon(map[string]client.Handler{"conversations.open": client.Result(openResult{Messages: msgs})}), 120, 40)
	h.keys(":")
	h.typeText("open general")
	h.keys("enter")
	h.settle()
	if h.count("files.fetch") != 0 {
		t.Fatalf("calls %v", h.calls())
	}
}

func TestInlineImageThatCannotBeRead(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "bad.png")
	if err := os.WriteFile(bad, []byte("not a png"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := openImages(t, imageDaemon(t, bad), image.HalfBlock)
	h.settle()
	if strings.Contains(h.screen(), "▀") || h.m.statusIsError {
		t.Fatalf("status %q:\n%s", h.m.status, h.screen())
	}
}

func TestSixelImagesStartOff(t *testing.T) {
	if newImageCache(image.Sixel).inline || !newImageCache(image.Kitty).inline || !newImageCache(image.HalfBlock).inline {
		t.Fatal("inline images should start on for kitty and half-block only")
	}
}

// imageTop finds the cell under the "[image]" tag where the picture's
// top-left corner goes.
func imageTop(t *testing.T, screen string) (row, col int) {
	t.Helper()
	for i, line := range strings.Split(screen, "\n") {
		if j := strings.Index(line, "[image]"); j >= 0 {
			return i + 1, ansi.StringWidth(line[:j])
		}
	}
	t.Fatalf("no image row:\n%s", screen)
	return 0, 0
}

func TestKittyImagesArePlacedOverTheRoomViewLeaves(t *testing.T) {
	path := writePNG(t)
	h := openImages(t, imageDaemon(t, path), image.Kitty)
	raw := h.settle()
	view := h.m.View().Content
	if strings.Contains(view, placeScheme) || strings.Contains(view, "\x1b_G") {
		t.Fatalf("marks or kitty data left in the view: %q", view)
	}
	row, col := imageTop(t, h.screen())
	head := image.ClearKitty + ansi.SaveCursor + ansi.CursorPosition(col+1, row+1) + "\x1b_Ga=T,f=100,q=2,C=1,c=16,r=6,"
	if !strings.HasPrefix(raw, head) || !strings.HasSuffix(raw, ansi.RestoreCursor) {
		t.Fatalf("raw %q", raw)
	}
	if again := h.settle(); again != "" {
		t.Fatalf("placed again without a change: %q", again)
	}
	lines := strings.Split(h.screen(), "\n")
	for _, l := range lines[row : row+6] {
		if cells := []rune(l)[col : col+16]; strings.TrimSpace(string(cells)) != "" {
			t.Fatalf("room not blank: %q", l)
		}
	}
	if strings.TrimSpace(string([]rune(lines[row+6])[col:col+16])) == "" {
		t.Fatalf("room taller than the picture:\n%s", h.screen())
	}
}

func TestKittyImagesClearedWhenTheyLeave(t *testing.T) {
	h := openImages(t, imageDaemon(t, writePNG(t)), image.Kitty)
	h.settle()
	h.keys("?") // help covers the panes
	if raw := h.settle(); raw != image.ClearKitty {
		t.Fatalf("raw %q", raw)
	}
	h.keys("esc")
	if raw := h.settle(); !strings.HasPrefix(raw, image.ClearKitty+ansi.SaveCursor) {
		t.Fatalf("not placed again: %q", raw)
	}
	h.m.Update(tea.WindowSizeMsg{Width: 120, Height: 40}) // its tick comes after the next View
	h.m.media.cache.ticking = false
	if raw := h.settle(); !strings.HasPrefix(raw, image.ClearKitty+ansi.SaveCursor) {
		t.Fatalf("not placed again after a resize: %q", raw)
	}
}

func TestSixelImagesArePlacedWithoutClearing(t *testing.T) {
	h := openImages(t, imageDaemon(t, writePNG(t)), image.Sixel)
	h.keys("I")
	raw := h.settle()
	if !strings.HasPrefix(raw, ansi.SaveCursor) || !strings.Contains(raw, "\x1bPq") {
		t.Fatalf("raw %q", raw)
	}
}

func TestImageTicksOnlyWhenThereAreImages(t *testing.T) {
	h := newHarness(t, daemon(nil), 120, 40)
	h.keys(":")
	h.typeText("open general")
	h.keys("enter")
	if h.m.media.cache.ticking || h.m.imagesActive() {
		t.Fatal("a conversation without images needs no tick")
	}
	start := time.Now()
	h.keys("k")
	if time.Since(start) > imageDelay {
		t.Fatal("a key waited for an image tick")
	}
}
