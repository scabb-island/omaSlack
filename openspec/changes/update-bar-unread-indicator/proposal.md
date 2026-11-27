## Why

The bar icon shows two stacked count pills (DMs and mentions; channels and threads). Issue #2 reports them as visual noise, and a ui-polish audit (2026-11-27) confirmed it and found three blocking problems: on vertical bars the pills are clipped ("47" shows as "4"), a lone pill tells DMs and channels apart by fill color only (indistinguishable on the "white" theme), and the main pill's text misses 4.5:1 contrast on rose-pine (3.84:1) and flexoki-light (4.42:1). No first-party Omarchy bar widget draws count pills: Tailscale uses a corner dot, Reminders keeps its count in the tooltip.

## What Changes

- **BREAKING (visual):** the two count pills become one unread dot at the icon's top-right corner (bar urgent color, ringed in the bar background), the same on horizontal and vertical bars. Counts move to the tooltip (per workspace) and the quick-view panel, which already show them.
- One dot for anything unread in scope, no separate channel marker.
- While a connection alert glyph shows (`auth_expired`, reconnecting, incompatible), the dot is hidden: one signal at a time.
- With "hide when nothing is unread" on, the icon stays visible while Do Not Disturb is on.
- Settings: `showChannelBadge` is retired; `unreadScope` becomes "What lights the dot" with readable options ("Everything", default; "DMs and mentions"). A saved `showChannelBadge: false` maps to "DMs and mentions".
- The quick panel's open marker centers on the glyph (`openPanelIndicatorItem`).
- No change to daemon calls, the Service API or state: `Badges.view` still computes the counts.

## Impact

- Specs: `gui-client` ("Bar icon with badges", "Settings from the manifest").
- Code: `plugin/lib/badges.js`, `plugin/BarIcon.qml`, `plugin/BarWidget.qml`, `plugin/manifest.json`; tests `plugin/tests/tst_badges.qml`, `plugin/tests/smoke/BarIconSmoke.qml`.
- Issue: #2. Audit report and renders: `ui-polish` run of 2026-11-27 (options A, B, C; B chosen).
- Archive order: this change MODIFIES requirements that `add-omaslack-v1` adds, so archive it after `add-omaslack-v1` (task 6.4).
