# Omarchy integration

How omaslack fits into Omarchy: the TUI theme, the launcher entry, the
shortcuts, the window rule, and living next to other Slack clients.
`omaslack setup` installs the per-user files, and `omaslack doctor`
checks most of it and prints the command that fixes each problem.

| File | Installed to | What it does |
| --- | --- | --- |
| `omaslack.toml.tpl` | `~/.config/omarchy/themed/omaslack.toml.tpl` | Omarchy renders it on every theme change to `~/.local/state/omarchy/current/theme/omaslack.toml` |
| `theme-set.d/omaslack` | `~/.config/omarchy/hooks/theme-set.d/omaslack` | Omarchy runs it after the change; it sends `SIGUSR1` to running `omaslack tui` processes, which reload the theme |
| `hypr/omaslack.lua` | `~/.config/hypr/omaslack.lua`, loaded from `hyprland.lua` | Floats and centers the plugin's window |
| `../desktop/omaslack.desktop` | `/usr/share/applications/omaslack.desktop` | Launcher entry that opens or focuses the TUI |

The package ships these files under `/usr/share/omaslack/` (the hook
as `theme-set.hook`, the rule as `hypr/omaslack.lua`) and never writes
to `$HOME`. `omaslack setup` does the per-user part.

## Setup

Run it once per user, after installing the package:

```bash
omaslack setup                 # theme template, hook, theme render, user service
omaslack setup --window-rule   # the same, plus the Hyprland window rule
omaslack setup --dry-run       # print what it would do, change nothing
```

It copies the template and the hook to the paths in the table above,
renders the current theme once with `omarchy-theme-refresh` (only when
the template changed or was never rendered), and runs
`systemctl --user enable --now omaslack`. Each step prints `OK`, `WARN`
or `FAIL`, as `omaslack doctor` does, and the exit code is 1 only when
a step failed.

- **Your changes are kept.** Setup records the checksum of every file it
  writes in `$XDG_STATE_HOME/omaslack/setup.json` (by default
  `~/.local/state/omaslack/setup.json`). A file it installed and you did
  not touch is updated to the packaged version; a file you changed is
  left alone, and setup prints a `diff -u` command to compare it with
  the new version.
- **Running it again changes nothing**: every step reports "already in
  place".
- **`--window-rule`** copies the rule to `~/.config/hypr/omaslack.lua`.
  Setup never edits your Hyprland config, so it prints the line to add
  to `~/.config/hypr/hyprland.lua` (see [Window rule](#window-rule)) and
  reports `OK` once that file loads it.
- **`--remove`** disables and stops the service and removes the template,
  hook and window rule if you did not change them (changed ones are kept
  and listed). It keeps your credentials and data, and prints how to
  remove them: `omaslack auth remove <team-id>` for each workspace in
  `omaslack auth list`, and `rm -rf ~/.local/share/omaslack` (or
  `$XDG_DATA_HOME/omaslack`). Run it before `pacman -Rns omaslack-bin`;
  after it, the files setup installed are still recognized from the
  manifest.
- **`--from <dir>`** takes the files from another directory instead of
  `/usr/share/omaslack`. A checkout's `packaging/` directory works too,
  so a development build can be set up without the package:
  `go run ./cmd/omaslack setup --from packaging --dry-run`.

`omaslack doctor` suggests `omaslack setup` when the template or the
hook is missing.

## Theme

These files let `omaslack tui` follow the Omarchy theme live (spec
tui-client, "Omarchy theme with live reload").

### Template

The template uses Omarchy's own variables (`{{ background }}`,
`{{ accent }}`, `{{ red }}`, ...), the same ones as the templates under
`$OMARCHY_PATH/default/themed/`. Omarchy resolves them from the theme's
`colors.toml`, including the legacy `color0`..`color15` names, so every
variable used here exists for every theme. The output has one key per TUI
color role:

| Role | Variable | Used for |
| --- | --- | --- |
| `background` | `background` | pane backgrounds |
| `foreground` | `foreground` | normal text |
| `muted` | `muted` | timestamps, hints, read conversations |
| `accent` | `accent` | focused pane border, selection, links |
| `border` | `lighter_background` | unfocused pane borders |
| `selection` | `selection` | background of the selected row |
| `unread` | `yellow` | conversations with unread messages |
| `mention` | `red` | mentions of the user, mention badges |
| `code` | `green` | inline code and code blocks |
| `quote` | `magenta` | block quote bar |
| `success` | `green` | connected state |
| `warning` | `yellow` | connecting, rate limited |
| `error` | `red` | disconnected, auth expired, failed sends |

Without the rendered file the TUI reads `colors.toml` directly with the
same mapping (`internal/tui/theme/roles.go`, which also falls back to the
legacy `colorN` names). A test keeps the template and that mapping in
sync. To customize the colors, edit the installed template; the change
applies on the next theme switch.

### Hook

The hook signals only processes named `omaslack` whose first argument is
`tui`, owned by the current user. `SIGUSR1` would terminate the daemon,
so it is never signaled. The hook exits 0 when no TUI is running.

A test (`internal/tui/theme/hook_test.go`) runs the hook against a TUI
and a daemon process and checks that only the TUI gets the signal.

## Launcher

`packaging/desktop/omaslack.desktop` puts "omaslack" in the Omarchy
launcher (Super + Space). It runs `omarchy-launch-or-focus-tui omaslack
tui`: the TUI opens in Omarchy's terminal with the app id
`org.omarchy.omaslack`, and picking the entry again focuses that window
instead of opening a second one.

## Shortcuts

omaslack binds no keys. These are suggestions for
`~/.config/hypr/bindings.lua`; the keys are free in Omarchy's defaults
(check yours with `omarchy menu keybindings --print`):

```lua
-- Show or hide the omaslack window (the plugin's panel).
o.bind("SUPER + ALT + M", "omaslack window", "omarchy-shell shell toggle ar.scabb.omaslack")
-- Open the omaslack TUI, or focus it if it is already open.
o.bind("SUPER + CTRL + M", "omaslack TUI", "omarchy-launch-or-focus-tui omaslack tui")
```

Hyprland reloads the config on save.

## Window rule

The plugin's full window is an omarchy-shell window (class
`org.quickshell`) titled exactly `omaslack`. Without a rule Hyprland
tiles it. `hypr/omaslack.lua` floats it, centered, at 1100x720:

```lua
o.window({ class = "^org\\.quickshell$", title = "^omaslack$" }, { float = true, center = true, size = { 1100, 720 } })
```

It is written for Hyprland's Lua config (Hyprland 0.56 and later, as
Omarchy ships it) and uses Omarchy's `o.window` helper, so floating
workspaces know the window floats on its own.

`omaslack setup --window-rule` copies it to `~/.config/hypr/omaslack.lua`.
Omarchy's Hyprland bootstrap (`$OMARCHY_PATH/default/hypr/bootstrap.lua`)
puts `~/.config/?.lua` on Lua's module path, so `hyprland.lua` loads that
file as the module `hypr.omaslack`. Add this line to
`~/.config/hypr/hyprland.lua`, after `require("default.hypr.omarchy")`:

```lua
if package.searchpath("hypr.omaslack", package.path) then require("hypr.omaslack") end
```

The guard keeps the config loading if the rule is removed later
(`omaslack setup --remove`); a plain `require("hypr.omaslack")` works
too while the file exists. Or paste the `o.window(...)` line itself at
the end of `hyprland.lua` and skip the copy.

## Coexistence with other Slack clients

omaslack is one more Slack client: Slack keeps the read state, so a
conversation read in omaslack is read in the official app and the other
way round. What each client does on its own is notify and show icons,
so running two of them means two notifications per message.

### The official Slack app (or Slack in a browser)

- Turn off its notifications so you do not get each one twice: in the
  app, Preferences → Notifications, set "Notify me about" to "Nothing"
  (or pause notifications on that device). For Slack in a browser or as
  an Omarchy web app, block notifications for `app.slack.com` in the
  browser's site settings.
- Hide its tray icon if the omaslack bar icon is enough: turn off the
  app's option to stay in the notification area when its window is
  closed (its name and place change between Slack versions; on Linux it
  is under Preferences → Advanced), or remove the tray widget's entry.
- The `desktop-session` provider reuses a session you are already signed
  in to — in the official Slack app or in a Chromium browser (Brave,
  Chrome, Chromium, Edge) at `app.slack.com`. `omaslack auth add
  --provider desktop-session` lists the sources it finds and reads the
  one you pick, so you need neither the official app nor a Slack app of
  your own; `omaslack doctor` shows which sources are available. Signing
  out there, or Slack revoking the session, signs omaslack out of that
  workspace too (`omaslack doctor` then shows it as `auth_expired`): add
  the workspace again to reuse a fresh session. The `internal-app`
  provider has its own tokens and does not depend on either.

### Other Slack plugins for the Omarchy shell

Another Slack plugin from the marketplace keeps its own connection to
Slack, its own bar icon and its own notifications. Both work side by
side, but you will see two badges and get duplicate notifications. Turn
off the other plugin's notifications in its settings, or disable it:

```bash
omarchy plugin list               # find its id
omarchy plugin disable <its id>
```

omaslack never changes another plugin's configuration, and its id
(`ar.scabb.omaslack`) and IPC target are its own, so the shortcuts above
do not clash with another plugin's.
