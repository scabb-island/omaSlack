.pragma library

// Text colors that stay readable on light and dark Omarchy themes alike
// (WCAG 2 relative luminance and contrast ratio). The shell's habit of
// Qt.darker(foreground) for secondary text darkens it on a dark theme but
// makes it stronger than the text on a light one; muted() moves it toward
// the background instead. readable() nudges a colored text (accent,
// urgent) toward the text color until it reads, and textOn() picks the
// text color for a colored fill (a badge). Colors are QML colors (r, g, b
// in 0–1); results are opaque.

// WCAG AA for body text.
var MIN_RATIO = 4.5
// How far muted() moves text toward the background, at most.
var MUTE = 0.35

function channel(v) { return v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4) }

function luminance(c) { return 0.2126 * channel(c.r) + 0.7152 * channel(c.g) + 0.0722 * channel(c.b) }

// ratio: the contrast ratio of a and b, 1 (none) to 21 (black on white).
function ratio(a, b) {
  var la = luminance(a)
  var lb = luminance(b)
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05)
}

// mix: a moved toward b by t (0: a, 1: b).
function mix(a, b, t) {
  return Qt.rgba(a.r + (b.r - a.r) * t, a.g + (b.g - a.g) * t, a.b + (b.b - a.b) * t, 1)
}

// muted: secondary text: fg moved up to MUTE toward bg, while it keeps
// MIN_RATIO on bg (text that has no contrast to spare stays as it is).
function muted(fg, bg) {
  var floor = Math.min(MIN_RATIO, ratio(fg, bg))
  var lo = 0
  var hi = MUTE
  // Contrast falls steadily as fg nears bg: bisect for the furthest t.
  for (var i = 0; i < 20; i++) {
    var t = (lo + hi) / 2
    if (ratio(mix(fg, bg, t), bg) >= floor) lo = t
    else hi = t
  }
  return mix(fg, bg, lo)
}

// readable: color as text on bg: itself when it reads, else moved toward
// fg (the theme's text color) until it reads, or as far as fg reads.
function readable(color, bg, fg) {
  var floor = Math.min(MIN_RATIO, ratio(fg, bg))
  for (var step = 0; step <= 20; step++) {
    var c = mix(color, fg, step / 20)
    if (ratio(c, bg) >= floor) return c
  }
  // Rounding can leave the last step a hair under the floor.
  return mix(color, fg, 1)
}

// textOn: a or b, whichever reads better on fill.
function textOn(fill, a, b) { return ratio(a, fill) >= ratio(b, fill) ? a : b }
