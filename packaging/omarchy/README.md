# Omarchy theme integration for the TUI

These files let `omaslack tui` follow the Omarchy theme live (spec
tui-client, "Omarchy theme with live reload").

| File | Installed to | What it does |
| --- | --- | --- |
| `omaslack.toml.tpl` | `~/.config/omarchy/themed/omaslack.toml.tpl` | Omarchy renders it on every theme change to `~/.local/state/omarchy/current/theme/omaslack.toml` |
| `theme-set.d/omaslack` | `~/.config/omarchy/hooks/theme-set.d/omaslack` | Omarchy runs it after the change; it sends `SIGUSR1` to running `omaslack tui` processes, which reload the theme |

The package ships both under `/usr/share/omaslack/`, and
`omaslack setup` (task 6.1a) copies them into the user's home, because a
package must not write to `$HOME`. Until then they can be copied by hand.

## Template

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

## Hook

The hook signals only processes named `omaslack` whose first argument is
`tui`, owned by the current user. `SIGUSR1` would terminate the daemon,
so it is never signaled. The hook exits 0 when no TUI is running.
