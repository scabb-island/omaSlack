package image

import (
	"bytes"
	"encoding/base64"
	"fmt"
	goimage "image"
	"image/color"
	"image/color/palette"
	"image/png"
	"strings"

	"golang.org/x/image/draw"
)

// scaled returns img resized to w×h. With opaque set it is flattened onto
// black first, for the encoders that cannot show transparency.
func scaled(img goimage.Image, w, h int, opaque bool) *goimage.RGBA {
	dst := goimage.NewRGBA(goimage.Rect(0, 0, w, h))
	op := draw.Src
	if opaque {
		draw.Draw(dst, dst.Bounds(), goimage.NewUniform(color.Black), goimage.Point{}, draw.Src)
		op = draw.Over
	}
	draw.BiLinear.Scale(dst, dst.Bounds(), img, img.Bounds(), op, nil)
	return dst
}

// halfBlock draws two vertical pixels per cell with "▀": the upper one in
// the foreground color, the lower one in the background color (24-bit
// SGR). The grid is the w×h image in cell units, CellWidth × CellHeight/2
// pixels per half-block. An odd last row leaves its lower half unpainted.
func halfBlock(img goimage.Image, w, h int) (string, error) {
	gw, gh := ceilDiv(w, CellWidth), ceilDiv(h, CellHeight/2)
	px := scaled(img, gw, gh, true)
	lines := make([]string, 0, ceilDiv(gh, 2))
	for y := 0; y < gh; y += 2 {
		lines = append(lines, halfBlockLine(px, y))
	}
	return strings.Join(lines, "\n"), nil
}

func halfBlockLine(px *goimage.RGBA, y int) string {
	var b strings.Builder
	var fg, bg string
	for x := range px.Bounds().Dx() {
		top, bottom := sgr(38, px.RGBAAt(x, y)), "\x1b[49m"
		if y+1 < px.Bounds().Dy() {
			bottom = sgr(48, px.RGBAAt(x, y+1))
		}
		if top != fg {
			b.WriteString(top)
		}
		if bottom != bg {
			b.WriteString(bottom)
		}
		fg, bg = top, bottom
		b.WriteString("▀")
	}
	b.WriteString("\x1b[0m")
	return b.String()
}

func sgr(layer int, c color.RGBA) string {
	return fmt.Sprintf("\x1b[%d;2;%d;%d;%dm", layer, c.R, c.G, c.B)
}

// sixelImage quantizes the w×h image to the 256-color Plan 9 palette with
// Floyd-Steinberg dithering, then encodes it as sixel.
func sixelImage(img goimage.Image, w, h int) (string, error) {
	px := scaled(img, w, h, true)
	pal := goimage.NewPaletted(px.Bounds(), palette.Plan9)
	draw.FloydSteinberg.Draw(pal, pal.Bounds(), px, goimage.Point{})
	return sixel(pal), nil
}

// sixel encodes a paletted image: raster attributes, the color registers
// it uses, then one band per six rows with a run-length encoded line per
// color, the empty tail of each line trimmed.
func sixel(img *goimage.Paletted) string {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	var b strings.Builder
	fmt.Fprintf(&b, "\x1bPq\"1;1;%d;%d", w, h)
	used := usedColors(img)
	for i, ok := range used {
		if ok {
			r, g, bl, _ := img.Palette[i].RGBA()
			fmt.Fprintf(&b, "#%d;2;%d;%d;%d", i, pct(r), pct(g), pct(bl))
		}
	}
	for top := 0; top < h; top += 6 {
		for i, ok := range used {
			if ok {
				writeSixelLine(&b, img, top, uint8(i)) // #nosec G115 -- palette index < 256
			}
		}
		b.WriteString("-")
	}
	b.WriteString("\x1b\\")
	return b.String()
}

func usedColors(img *goimage.Paletted) []bool {
	used := make([]bool, len(img.Palette))
	for _, i := range img.Pix {
		used[i] = true
	}
	return used
}

func pct(v uint32) uint32 { return (v*100 + 0xffff/2) / 0xffff }

// writeSixelLine writes color i's line for the band starting at row top,
// or nothing when the band does not use it.
func writeSixelLine(b *strings.Builder, img *goimage.Paletted, top int, i uint8) {
	line := make([]byte, img.Bounds().Dx())
	for x := range line {
		line[x] = sixelBits(img, x, top, i)
	}
	trimmed := bytes.TrimRight(line, "?")
	if len(trimmed) == 0 {
		return
	}
	fmt.Fprintf(b, "#%d", i)
	writeRuns(b, trimmed)
	b.WriteString("$")
}

func sixelBits(img *goimage.Paletted, x, top int, i uint8) byte {
	bits := byte(0)
	for k := range 6 {
		y := top + k
		if y < img.Bounds().Dy() && img.ColorIndexAt(x, y) == i {
			bits |= 1 << k
		}
	}
	return '?' + bits
}

// writeRuns writes line with runs of four or more equal characters as
// "!<count><char>".
func writeRuns(b *strings.Builder, line []byte) {
	for len(line) > 0 {
		n := 1
		for n < len(line) && line[n] == line[0] {
			n++
		}
		if n >= 4 {
			fmt.Fprintf(b, "!%d%c", n, line[0])
		} else {
			b.Write(line[:n])
		}
		line = line[n:]
	}
}

// kitty sends the w×h image as PNG (transparency kept) with the kitty
// graphics protocol, placed over the cells Size reports.
func kitty(img goimage.Image, w, h int) (string, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, scaled(img, w, h, false)); err != nil {
		return "", fmt.Errorf("image: %w", err)
	}
	c, r := cells(w, h)
	return kittyEscape(base64.StdEncoding.EncodeToString(buf.Bytes()), c, r), nil
}

// kittyChunkSize is the protocol's maximum payload per escape sequence.
const kittyChunkSize = 4096

// kittyEscape transmits and displays the base64 PNG payload in chunks
// (a=T, f=100) over cols×rows cells (c, r), with no reply from the
// terminal (q=2) and without moving the cursor (C=1).
func kittyEscape(payload string, cols, rows int) string {
	var b strings.Builder
	control := fmt.Sprintf("a=T,f=100,q=2,C=1,c=%d,r=%d,", cols, rows)
	for {
		n := min(kittyChunkSize, len(payload))
		more := 0
		if n < len(payload) {
			more = 1
		}
		fmt.Fprintf(&b, "\x1b_G%sm=%d;%s\x1b\\", control, more, payload[:n])
		payload, control = payload[n:], ""
		if more == 0 {
			return b.String()
		}
	}
}
