import QtQuick
import QtTest
import "../lib/contrast.js" as C

// lib/contrast.js: text colors that stay readable on light and dark
// Omarchy themes. The theme colors are real ones (colors.toml).
TestCase {
  name: "Contrast"

  readonly property color black: "#000000"
  readonly property color white: "#ffffff"
  // catppuccin-latte (light).
  readonly property color latteFg: "#4c4f69"
  readonly property color latteBg: "#eff1f5"
  readonly property color latteAccent: "#1e66f5"
  // tokyo-night (dark).
  readonly property color tokyoFg: "#a9b1d6"
  readonly property color tokyoBg: "#1a1b26"
  readonly property color tokyoAccent: "#7aa2f7"
  // rose-pine: an accent under 4.5:1 on its background.
  readonly property color roseFg: "#575279"
  readonly property color roseBg: "#faf4ed"
  readonly property color roseAccent: "#56949f"
  // miasma: red (urgent) under 2.5:1 on its background.
  readonly property color miasmaFg: "#c2c2b0"
  readonly property color miasmaBg: "#222222"
  readonly property color miasmaRed: "#685742"

  function test_ratio_is_the_wcag_contrast_ratio() {
    fuzzyCompare(C.ratio(black, white), 21, 0.01)
    fuzzyCompare(C.ratio(white, black), 21, 0.01)
    fuzzyCompare(C.ratio(latteBg, latteBg), 1, 0.001)
  }

  function test_muted_text_is_quieter_than_the_text_on_light_and_dark() {
    var pairs = [[latteFg, latteBg], [tokyoFg, tokyoBg], [black, white], [white, black]]
    for (var i = 0; i < pairs.length; i++) {
      var fg = pairs[i][0], bg = pairs[i][1]
      var m = C.muted(fg, bg)
      verify(C.ratio(m, bg) < C.ratio(fg, bg), "muted is not quieter than " + fg)
      verify(C.ratio(m, bg) >= C.MIN_RATIO - 0.01, "muted " + m + " is unreadable on " + bg)
    }
  }

  function test_muted_leaves_text_alone_when_it_has_no_contrast_to_spare() {
    var fg = C.mix(latteBg, black, 0.5)
    verify(C.ratio(fg, latteBg) < C.MIN_RATIO)
    compare(C.ratio(C.muted(fg, latteBg), latteBg), C.ratio(fg, latteBg))
  }

  function test_readable_keeps_a_color_that_reads_and_darkens_one_that_does_not() {
    verify(Qt.colorEqual(C.readable(tokyoAccent, tokyoBg, tokyoFg), tokyoAccent), "a readable accent changed")
    verify(C.ratio(C.readable(latteAccent, latteBg, latteFg), latteBg) >= C.MIN_RATIO, "latte's accent is unreadable")
    var rose = C.readable(roseAccent, roseBg, roseFg)
    verify(C.ratio(roseAccent, roseBg) < C.MIN_RATIO)
    verify(C.ratio(rose, roseBg) >= C.MIN_RATIO, "rose-pine's accent is still unreadable: " + rose)
    var red = C.readable(miasmaRed, miasmaBg, miasmaFg)
    verify(C.ratio(red, miasmaBg) >= C.MIN_RATIO, "miasma's red is still unreadable: " + red)
  }

  function test_readable_goes_no_further_than_the_text_color() {
    var dim = C.mix(latteBg, latteFg, 0.2)
    var fg = C.mix(latteBg, latteFg, 0.5)
    verify(C.ratio(fg, latteBg) < C.MIN_RATIO)
    verify(Qt.colorEqual(C.readable(dim, latteBg, fg), fg), "readable went past the text color")
  }

  function test_text_on_a_fill_takes_the_color_that_contrasts_more() {
    verify(Qt.colorEqual(C.textOn(latteAccent, latteBg, latteFg), latteBg), "badge text on latte's accent")
    verify(Qt.colorEqual(C.textOn(white, latteBg, latteFg), latteFg), "badge text on white")
  }
}
