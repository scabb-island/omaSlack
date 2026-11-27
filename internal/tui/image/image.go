// Package image shows images inline in the terminal (spec tui-client,
// "Images in the terminal"; task 3.5): the kitty graphics protocol or
// sixel when the terminal supports them, half-block characters otherwise.
// Images come from files.fetch, a local path in the daemon's cache.
package image

import (
	"errors"
	"fmt"
	goimage "image"
	_ "image/gif" // GIF decoder; Decode returns the first frame
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"

	_ "golang.org/x/image/webp" // Slack serves WebP thumbnails too
)

// Protocol is how a terminal can show images.
type Protocol uint8

const (
	HalfBlock Protocol = iota // works everywhere (Alacritty)
	Sixel
	Kitty
)

// The cell size Render assumes, in pixels. Terminals do not say theirs
// without a query, and most cells are about twice as tall as wide, so
// a half-block character covers a square of CellWidth × CellHeight/2.
// Kitty output names its cell count, so the terminal scales it to fit
// exactly; sixel is drawn at its pixel size and may come out a little
// larger or smaller than the cells Size reports.
const (
	CellWidth  = 10
	CellHeight = 20
)

// MaxPixels bounds the images Render decodes (50 megapixels, about 200 MB
// decoded). Larger images are refused with ErrTooLarge before decoding,
// from their header alone, so a huge file costs neither memory nor time.
const MaxPixels = 50_000_000

// ErrTooLarge means the image has more than MaxPixels pixels.
var ErrTooLarge = errors.New("image: too large to show inline")

// ClearKitty deletes every kitty image placement on screen. The TUI sends
// it before redrawing the images, since kitty images outlive the text
// around them.
const ClearKitty = "\x1b_Ga=d,d=A,q=2\x1b\\"

// Render returns the escape sequence or half-block text that shows the
// image at path within cols×rows cells (task 3.5). The image is scaled
// down to fit, keeping its aspect ratio, and never scaled up. It covers
// the cells Size reports. Half-block output is that many lines joined by
// "\n"; kitty and sixel output is one escape sequence to print with the
// cursor at the image's top-left cell.
func Render(path string, p Protocol, cols, rows int) (string, error) {
	enc, ok := encoders[p]
	if !ok {
		return "", fmt.Errorf("image: unknown protocol %d", p)
	}
	img, err := decode(path, cols, rows)
	if err != nil {
		return "", err
	}
	w, h := fit(img.Bounds().Dx(), img.Bounds().Dy(), cols, rows)
	return enc(img, w, h)
}

// Size returns the cells Render(path, _, cols, rows) covers, reading only
// the image header.
func Size(path string, cols, rows int) (c, r int, err error) {
	f, cfg, err := open(path, cols, rows)
	if err != nil {
		return 0, 0, err
	}
	_ = f.Close()
	c, r = cells(fit(cfg.Width, cfg.Height, cols, rows))
	return c, r, nil
}

var encoders = map[Protocol]func(goimage.Image, int, int) (string, error){
	HalfBlock: halfBlock,
	Sixel:     sixelImage,
	Kitty:     kitty,
}

func decode(path string, cols, rows int) (goimage.Image, error) {
	f, _, err := open(path, cols, rows)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	img, _, err := goimage.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("image: %s: %w", path, err)
	}
	return img, nil
}

// open checks the arguments and the image header, and returns the file
// rewound for decoding.
func open(path string, cols, rows int) (*os.File, goimage.Config, error) {
	if cols <= 0 || rows <= 0 {
		return nil, goimage.Config{}, fmt.Errorf("image: no room: %d×%d cells", cols, rows)
	}
	f, err := os.Open(path) // #nosec G304 -- a file in the daemon's cache
	if err != nil {
		return nil, goimage.Config{}, fmt.Errorf("image: %w", err)
	}
	cfg, err := checkHeader(f)
	if err != nil {
		_ = f.Close()
		return nil, goimage.Config{}, fmt.Errorf("image: %s: %w", path, err)
	}
	return f, cfg, nil
}

func checkHeader(f *os.File) (goimage.Config, error) {
	cfg, _, err := goimage.DecodeConfig(f)
	switch {
	case err != nil:
		return cfg, err
	case cfg.Width <= 0 || cfg.Height <= 0:
		return cfg, errors.New("empty image")
	case int64(cfg.Width)*int64(cfg.Height) > MaxPixels:
		return cfg, fmt.Errorf("%w: %d×%d", ErrTooLarge, cfg.Width, cfg.Height)
	}
	_, err = f.Seek(0, io.SeekStart)
	return cfg, err
}

// fit returns the pixel size of a w×h image scaled down to fit cols×rows
// cells, keeping its aspect ratio; at least 1×1.
func fit(w, h, cols, rows int) (fw, fh int) {
	scale := min(1, float64(cols*CellWidth)/float64(w), float64(rows*CellHeight)/float64(h))
	return max(1, int(float64(w)*scale+0.5)), max(1, int(float64(h)*scale+0.5))
}

// cells is how many cells a w×h pixel image covers.
func cells(w, h int) (c, r int) {
	return ceilDiv(w, CellWidth), ceilDiv(h, CellHeight)
}

func ceilDiv(a, b int) int { return (a + b - 1) / b }
