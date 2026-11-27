## MODIFIED Requirements

### Requirement: Bar icon with badges
`BarWidget.qml` SHALL show an icon that follows the bar's color and one unread dot at the icon's corner, in the bar's urgent color with a ring in the bar background so it reads on any theme, lit by any unread conversation in scope. It SHALL NOT draw unread counts in the bar: the counts SHALL be in the tooltip (per workspace) and in the quick-view panel. It SHALL also show the connection state (connected, reconnecting, error, `auth_expired`) and DND; while a connection alert glyph is shown, the dot SHALL be hidden so only one signal shows at a time. The same corner dot SHALL be used on horizontal and vertical bars, so the icon never grows past its slot.

#### Scenario: Daemon down
- **WHEN** the daemon does not answer
- **THEN** the icon dims and the tooltip says how to start it

#### Scenario: Clicks
- **WHEN** the user left-clicks the icon
- **THEN** the quick-view Panel opens. A right click, or picking an item in the panel, opens the full window

#### Scenario: Unread anywhere
- **WHEN** there is at least one unread conversation in scope (a DM, a mention or, with the scope "Everything", a channel or thread)
- **THEN** the dot shows at the icon's corner and the tooltip lists each workspace's counts

#### Scenario: Alert wins
- **WHEN** a workspace's sign-in expired or it is reconnecting
- **THEN** the alert glyph shows and the dot does not, and the tooltip still lists the unread counts

#### Scenario: Vertical bar
- **WHEN** the bar is vertical
- **THEN** the icon and its dot fit the bar's width with nothing clipped

### Requirement: Settings from the manifest
Widget options (what lights the unread dot, preferred client to open, hiding the icon when nothing is pending) SHALL be declared in `barWidget.schema[]` with values in `barWidget.defaults` and readable labels, so they can be edited from the shell settings. While Do Not Disturb is on, the icon SHALL stay visible even when nothing is unread.

#### Scenario: Hide when nothing is pending
- **WHEN** "hide when there is nothing unread" is on, there is nothing unread and Do Not Disturb is off
- **THEN** the icon takes no space in the bar

#### Scenario: Do Not Disturb stays visible
- **WHEN** "hide when there is nothing unread" is on, there is nothing unread and Do Not Disturb is on
- **THEN** the icon stays in the bar showing the DND state

#### Scenario: Older settings
- **WHEN** a shell.json still has `showChannelBadge: false` from an earlier version
- **THEN** the widget treats it as the scope "DMs and mentions"
