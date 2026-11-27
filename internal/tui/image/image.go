// Package image shows images inline in the terminal (spec tui-client,
// "Images in the terminal"; task 3.5): the kitty graphics protocol or
// sixel when the terminal supports them, half-block characters otherwise.
// Images come from files.fetch, a local path in the daemon's cache.
package image

import "errors"

// Protocol is how a terminal can show images.
type Protocol uint8

const (
	HalfBlock Protocol = iota // works everywhere (Alacritty)
	Sixel
	Kitty
)

// Render returns the escape sequence or half-block text that shows the
// image at path within cols×rows cells (task 3.5).
func Render(path string, p Protocol, cols, rows int) (string, error) {
	return "", errors.New("image: not implemented yet (task 3.5)")
}
