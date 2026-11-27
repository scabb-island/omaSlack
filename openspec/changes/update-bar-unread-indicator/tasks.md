## 1. Indicator
- [x] 1.1 `Badges.view`: a `dot` boolean (anything unread in scope, false while an alert glyph shows) next to the existing counts; tests first in `tst_badges.qml`
- [x] 1.2 `Badges.layout`: the dot's corner position inside the slot for horizontal and vertical bars (`x + w <= slotWidth`), the slot never grows; replaces the pill layout
- [x] 1.3 `BarIcon.qml`: draw the dot (diameter `Style.space(6)`, ring 1 px in the bar background, color `bar.urgent`, inherits the button's opacity and press scale; no animation); remove both pills
- [x] 1.4 Tooltip: per-workspace counts stay as today (DMs, mentions, channels, threads)

## 2. States and settings
- [x] 2.1 DND keeps the icon visible with `hideWhenEmpty`
- [x] 2.2 Manifest: retire `showChannelBadge`; `unreadScope` labeled "What lights the dot" with options "Everything" (default) and "DMs and mentions"; map a saved `showChannelBadge: false` to "DMs and mentions"
- [x] 2.3 `BarWidget.qml`: `openPanelIndicatorItem` points at the glyph button

## 3. Verification
- [x] 3.1 `BarIconSmoke`: dot on/off per state, alert hides it, vertical bar fits, DND visible with hideWhenEmpty
- [x] 3.2 Renders at 1.0 / 1.2 / 2.0, horizontal and vertical, dark and light (incl. white, rose-pine, flexoki-light); grayscale check that the dot reads without color
- [ ] 3.3 Live check in omarchy-shell (with the user's consent) and screenshots attached to issue #2
- [x] 3.4 `make qmllint`, `make qmltest`, `omarchy plugin validate plugin`, `openspec validate --all --strict`
