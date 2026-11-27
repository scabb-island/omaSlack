package image

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"flag"
	"hash/crc32"
	goimage "image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// quadrants is a w×h image with four solid quadrants: red, green on top,
// blue, white below. Scaled down, each quadrant keeps its color, so the
// golden output stays readable.
func quadrants(w, h int) *goimage.RGBA {
	img := goimage.NewRGBA(goimage.Rect(0, 0, w, h))
	colors := [2][2]color.RGBA{
		{{255, 0, 0, 255}, {0, 255, 0, 255}},
		{{0, 0, 255, 255}, {255, 255, 255, 255}},
	}
	for y := range h {
		for x := range w {
			img.Set(x, y, colors[y*2/h][x*2/w])
		}
	}
	return img
}

func writePNG(t *testing.T, img goimage.Image) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "img.png")
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(p, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("%s differs from %s:\ngot  %q\nwant %q", name, p, got, want)
	}
}

func TestFit(t *testing.T) {
	cases := []struct {
		w, h, cols, rows int
		tw, th           int
	}{
		{20, 40, 80, 24, 20, 40},     // small: never upscaled
		{1000, 500, 10, 10, 100, 50}, // wide: width bound (10 cols × 10 px)
		{500, 1000, 40, 5, 50, 100},  // tall: height bound (5 rows × 20 px)
		{3000, 1, 10, 10, 100, 1},    // a line keeps at least one pixel
	}
	for _, c := range cases {
		tw, th := fit(c.w, c.h, c.cols, c.rows)
		if tw != c.tw || th != c.th {
			t.Errorf("fit(%d,%d,%d,%d) = %d,%d want %d,%d", c.w, c.h, c.cols, c.rows, tw, th, c.tw, c.th)
		}
	}
}

func TestSize(t *testing.T) {
	path := writePNG(t, quadrants(25, 30))
	c, r, err := Size(path, 80, 24)
	if err != nil || c != 3 || r != 2 {
		t.Fatalf("Size = %d,%d,%v want 3,2", c, r, err)
	}
	if c, r, _ := Size(writePNG(t, quadrants(1000, 500)), 10, 10); c != 10 || r != 3 {
		t.Fatalf("Size of a large image = %d,%d want 10,3", c, r)
	}
}

func TestRenderHalfBlockGolden(t *testing.T) {
	out, err := Render(writePNG(t, quadrants(20, 40)), HalfBlock, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "halfblock", out)
	if lines := strings.Split(out, "\n"); len(lines) != 2 || strings.Count(lines[0], "▀") != 2 {
		t.Fatalf("want 2 lines of 2 cells, got %q", out)
	}
}

func TestRenderHalfBlockOddHeightLeavesBottomHalfEmpty(t *testing.T) {
	red := goimage.NewUniform(color.RGBA{255, 0, 0, 255})
	img := goimage.NewRGBA(goimage.Rect(0, 0, 10, 10))
	draw.Draw(img, img.Bounds(), red, goimage.Point{}, draw.Src)
	out, err := Render(writePNG(t, img), HalfBlock, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if out != "\x1b[38;2;255;0;0m\x1b[49m▀\x1b[0m" {
		t.Fatalf("out = %q", out)
	}
}

func TestRenderSixelGolden(t *testing.T) {
	out, err := Render(writePNG(t, quadrants(4, 12)), Sixel, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "sixel", out)
	if !strings.HasPrefix(out, "\x1bPq\"1;1;4;12") || !strings.HasSuffix(out, "\x1b\\") {
		t.Fatalf("not a sixel image: %q", out)
	}
}

func TestSixelRunLength(t *testing.T) {
	img := goimage.NewPaletted(goimage.Rect(0, 0, 9, 1), color.Palette{color.Black, color.White})
	for x := range 9 {
		img.SetColorIndex(x, 0, 1)
	}
	if out := sixel(img); !strings.Contains(out, "#1!9@$") {
		t.Fatalf("want a run of 9, got %q", out)
	}
}

var kittyChunk = regexp.MustCompile(`\x1b_G([^;]*);([^\x1b]*)\x1b\\`)

func TestRenderKitty(t *testing.T) {
	out, err := Render(writePNG(t, quadrants(20, 40)), Kitty, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "kitty", out)
	m := kittyChunk.FindAllStringSubmatch(out, -1)
	if len(m) != 1 || m[0][1] != "a=T,f=100,q=2,C=1,c=2,r=2,m=0" {
		t.Fatalf("chunks = %q", m)
	}
	data, err := base64.StdEncoding.DecodeString(m[0][2])
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil || img.Bounds().Dx() != 20 || img.Bounds().Dy() != 40 {
		t.Fatalf("payload: %v %v", img.Bounds(), err)
	}
}

func TestKittyChunksAt4096(t *testing.T) {
	payload := strings.Repeat("A", 4096*2+10)
	m := kittyChunk.FindAllStringSubmatch(kittyEscape(payload, 3, 4), -1)
	if len(m) != 3 {
		t.Fatalf("want 3 chunks, got %d", len(m))
	}
	if m[0][1] != "a=T,f=100,q=2,C=1,c=3,r=4,m=1" || m[1][1] != "m=1" || m[2][1] != "m=0" {
		t.Fatalf("controls = %q %q %q", m[0][1], m[1][1], m[2][1])
	}
	if len(m[0][2]) != 4096 || len(m[1][2]) != 4096 || len(m[2][2]) != 10 {
		t.Fatal("chunk sizes")
	}
}

func TestRenderDecodesJPEGAndGIF(t *testing.T) {
	src := quadrants(16, 16)
	for name, enc := range map[string]func(*bytes.Buffer) error{
		"img.jpg": func(b *bytes.Buffer) error { return jpeg.Encode(b, src, nil) },
		"img.gif": func(b *bytes.Buffer) error { return gif.Encode(b, src, nil) },
	} {
		var buf bytes.Buffer
		if err := enc(&buf); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(p, buf.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		if out, err := Render(p, HalfBlock, 80, 24); err != nil || strings.Count(out, "▀") != 2 {
			t.Errorf("%s: %q, %v", name, out, err)
		}
	}
}

// testdata/red16.webp is a 36-byte lossless 16×16 red WebP, made with
// `magick -size 16x16 xc:red -define webp:lossless=true red16.webp`: the
// standard library has no WebP encoder to generate it in the test.
func TestRenderDecodesWebP(t *testing.T) {
	out, err := Render(filepath.Join("testdata", "red16.webp"), HalfBlock, 80, 24)
	if err != nil || out != "\x1b[38;2;255;0;0m\x1b[48;2;255;0;0m▀▀\x1b[0m" {
		t.Fatalf("out = %q, %v", out, err)
	}
}

// hugePNG writes only a PNG signature and an IHDR chunk declaring w×h:
// enough for DecodeConfig, so the size check runs without the pixels.
func hugePNG(t *testing.T, w, h uint32) string {
	t.Helper()
	ihdr := binary.BigEndian.AppendUint32([]byte("IHDR"), w)
	ihdr = binary.BigEndian.AppendUint32(ihdr, h)
	ihdr = append(ihdr, 8, 6, 0, 0, 0)
	b := []byte("\x89PNG\r\n\x1a\n")
	b = binary.BigEndian.AppendUint32(b, uint32(len(ihdr)-4))
	b = append(b, ihdr...)
	b = binary.BigEndian.AppendUint32(b, crc32.ChecksumIEEE(ihdr))
	p := filepath.Join(t.TempDir(), "huge.png")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRenderRefusesHugeImages(t *testing.T) {
	_, err := Render(hugePNG(t, 10000, 5001), HalfBlock, 80, 24)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := Size(hugePNG(t, 10000, 5001), 80, 24); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Size err = %v", err)
	}
}

func TestRenderErrors(t *testing.T) {
	notImage := filepath.Join(t.TempDir(), "x.txt")
	if err := os.WriteFile(notImage, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	png := writePNG(t, quadrants(4, 4))
	for name, call := range map[string]func() error{
		"missing":   func() error { _, err := Render(filepath.Join(t.TempDir(), "nope"), HalfBlock, 8, 8); return err },
		"not image": func() error { _, err := Render(notImage, HalfBlock, 8, 8); return err },
		"no cols":   func() error { _, err := Render(png, HalfBlock, 0, 8); return err },
		"no rows":   func() error { _, _, err := Size(png, 8, 0); return err },
		"protocol":  func() error { _, err := Render(png, Protocol(9), 8, 8); return err },
	} {
		if call() == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}
