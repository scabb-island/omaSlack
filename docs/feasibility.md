# Feasibility — omaslack: "Omarchy Way" Slack with a UI and a TUI

Date: 2026-11-26 · Status: proposal · Specs: [`openspec/changes/add-omaslack-v1`](../openspec/changes/add-omaslack-v1/)

## 1. Verdict

**Feasible**, with known and bounded risks.

The requirement that it work from both a UI and a TUI is met with a **local daemon + two thin clients** architecture:

- a **QML plugin** for Omarchy's Quattro shell, with a bar icon, a panel and a window;
- a **Go TUI** built on bubbletea.

Both share state (unread counts, drafts, read marks) through a unix socket.

None of the pieces is experimental:

- slk (MIT) shows that Go + bubbletea + the desktop app protocol make a complete Slack client.
- The five Slack plugins in the marketplace show that the shell can host a QML chat window, with a bar icon, a Service and helper processes.

The real risk is not technical but **platform-related**:

- Slack's unofficial protocol and its terms of service;
- the limits of the official API;
- an Omarchy plugin API that is still young.

All three are mitigated by the design (section 6).

## 2. Analysis of slk (gammons/slk)

Source: `github.com/gammons/slk` (cloned on 2026-11-26).

| Aspect | Finding | Source |
|---|---|---|
| Stack | Go 1.26, bubbletea/lipgloss/bubbles v2, slack-go 0.23, modernc sqlite, gorilla/websocket, godbus | `go.mod` |
| Size | ~107k lines of Go excluding tests (the wiki says ~9.3k; it is out of date) | `wc -l` over `*.go` |
| Layers | UI (bubbletea) → Service → Client (Web API + browser WebSocket) → Data (SQLite, TOML, tokens) | `wiki/Architecture.md` |
| Ports | The interfaces the TUI consumes live in one file (512 lines) | `internal/core/ports.go` |
| Auth | Reads the desktop app session (cookies, leveldb, Secret Service and KWallet) or a browser cURL; xoxc/xoxd tokens | `internal/slackdesktop/`, `README.md` |
| Real time | The browser protocol's WebSocket (not RTM or Socket Mode), reconnecting with 1–30 s backoff | `docs/STATUS.md` |
| IPC | A control socket, only for opening permalinks | `cmd/slk/control_socket.go` |
| Themes | 59 themes, TOML in `~/.config/slk/themes/`, reload on `SIGUSR1` | `wiki/Configuration.md`, `cmd/slk/themereload_unix.go` |
| Non-goals | Huddles, Slack Connect, Workflow Builder, app and emoji management | `wiki/Tradeoffs-and-Non-Goals.md` |
| License | MIT | `LICENSE` |

**What we take (with MIT attribution):**
- the desktop session reader;
- the WebSocket client and event handling;
- mrkdwn rendering;
- the image pipeline (kitty, sixel, half-block);
- the key map design.

**Why we do not fork it:** slk's logic is coupled to bubbletea, because WebSocket events travel as `tea.Msg` inside `cmd/slk`. Turning it into a daemon amounts to rewriting the service layer, and the fork would drift from upstream for good.

## 3. What already exists in the Omarchy marketplace

Source: `omacom/omarchy-plugin-marketplace/registry.json` (7,091 sources). The five Slack plugins were cloned and read:

| Plugin | What it is | Connection | Real time | UI | TUI |
|---|---|---|---|---|---|
| `janrenz.omarchy.slack` | Bar, dropdown and `FloatingWindow` in QML, with a Python helper (~14k lines of QML/Py) | Official API (OAuth with the bundled app or your own) | No: polls every 120 s, designed around 1 `conversations.history` call per minute | Yes | No |
| `bottelet.slack` | Bar and panel with a window | Official API (own token or OAuth through a worker) | No: polling | Yes | No |
| `petrzpav.slack` | Python TUI + `slack-sync.service` + bar widget | Internal app with Socket Mode | Yes | Bar only | Yes |
| `leonavas.slack` | Bar widget that drives the "slick" TUI (Rust) from the scratchpad | Delegated to slick | Depends on slick | Bar only | External |
| `io.github.thisisgm.slack` | Availability switcher (Available, Focus, Away) | Own app, 4 scopes | No | Minimal panel | No |

**The gap omaslack fills:** no plugin offers **one engine** feeding both a full native UI **and** a TUI with synchronized, real-time state. `janrenz.omarchy.slack/GAPS.md` (SLACK-1) documents what happens without a central store: the bar keeps showing an unread item for up to 120 s after the window marked it read. omaslack avoids that by design, because the daemon is the single source of truth.

**Patterns worth reusing:**
- A single QML Service that launches a helper (`janrenz`, `gardnmi/omarchy-irc`).
- `FloatingWindow` as a real Hyprland window (`janrenz`).
- Settings in `barWidget.schema[]`.
- Tokens in `secret-tool` (`thisisgm`, `bottelet`).
- Parking in the scratchpad (`leonavas`), an idea for the TUI.

## 4. The Omarchy plugin contract (summary)

Sources: `plugins.omarchy.org/develop.html`, `publish.html`, `/usr/share/omarchy/shell/` and `omarchy plugin list --json` on an Omarchy machine.

- `manifest.json`: `schemaVersion`, `id` (namespaced, never `omarchy.*`), `name`, `version`, `author`, `description`, `kinds` (`bar-widget`, `panel`, `overlay`, `menu`, `service`, `bar`), `entryPoints` and `barWidget{displayName, category, defaults, schema[]}`.
- QML on Quickshell (`Quickshell`, `Quickshell.Io`, `qs.Commons`, `qs.Ui`), with colors from the `Style` singleton.
- IPC is `omarchy-shell shell toggle|summon|hide <id>`. Lifecycle: `omarchy plugin add|remove|validate|list`.
- **Runs unsandboxed** inside the shell process. It must not start another Quickshell.
- Third-party theming: `~/.config/omarchy/themed/*.tpl` templates (variables `{{ foreground_strip }}`, `{{ red_strip }}`, …), rendered into `~/.local/state/omarchy/current/theme/`, plus `~/.config/omarchy/hooks/theme-set.d/` hooks.
- Terminal apps: `omarchy-launch-tui` and `omarchy-launch-or-focus-tui`.

## 5. Connecting to Slack: the two providers

| | `desktop-session` | `internal-app` |
|---|---|---|
| Credential | xoxc + xoxd cookie (desktop app or browser cURL) | xoxp user token + xapp app token |
| Setup | None if the desktop app is signed in | Create an app from the manifest and install it in the workspace (may need an admin) |
| Real time | The web client's WebSocket | Socket Mode |
| Rate limits | The web client's | Full (the 1-call-per-minute limit does not apply to internal apps) |
| Features | Everything slk has (sections, activity, visible huddles, etc.) | What the public Web API offers (no native sections, some internal data missing) |
| Terms of service | **Unofficial**: may violate Slack's terms. Requires explicit consent | Official |
| Breakage risk | Medium: Slack may change the protocol | Low |

Whatever a provider does not support is declared in `Capabilities`, and clients hide it (spec `slack-connection`).

## 6. Risks and mitigations

| Risk | Mitigation |
|---|---|
| Slack changes or blocks the web client protocol | Isolated `Provider`, tests with recorded events, and a one-command switch to `internal-app` |
| Terms of service or corporate policy (relevant for work workspaces) | Explicit consent; the internal-app path documented for admins to approve |
| Official API rate limits | `RateBudget` with priorities and `Retry-After`; internal app recommended |
| Young plugin API (`schemaVersion` 1) | Minimal QML surface, logic in the daemon, and `omarchy plugin validate` + `qmllint` in `make local` |
| QML performance with long histories | Virtualized `ListView`, pagination and mrkdwn rendered by the daemon |
| Images in the TUI (Alacritty has no graphics) | kitty or sixel when the terminal supports them, half-block fallback and an external viewer |
| Unsandboxed plugins | QML never touches tokens or the Slack network |
| The "Slack" trademark | Own name (omaslack); "Slack" used only to describe interoperability, as slk does |
| Competition in the marketplace (5 plugins) | Differentiate on UI + TUI, real time and a single state |

### Field report (2026-11-26): a workspace that rejects reused sessions

The first risk above showed up on a real workspace. Slack answered
`invalid_auth` to every reused session: the xoxc token and d cookie read from
the signed-in desktop app, a fresh "Copy as cURL" from the browser, and even
the exact browser request replayed byte for byte (from Go and from the system
curl, over HTTP/2 and HTTP/1.1). The same request worked inside the browser.

What it means: omaslack parses the credentials correctly; that workspace binds
its sessions to Slack's own client, through the TLS fingerprint, device-bound
credentials or a workspace policy (approved clients, session restrictions,
EMM). No change to how omaslack reads or sends the session can get around
it, and the `desktop-session` provider cannot serve such workspaces.

Mitigation: the official `internal-app` provider
([setup guide](internal-app-setup.md)). `omaslack auth add` now checks every
desktop-session credential with `auth.test` before storing it, the desktop app's
as well as a pasted one, and when Slack rejects it, it stores nothing and
explains this case and points to `internal-app`.

## 7. Estimated effort

**A rough estimate**, for one person with AI assistance, porting pieces of slk. It is not based on measurements.

| Milestone | Content | Weeks |
|---|---|---|
| M0 | Skeleton, CI, minimal manifest | 0.5 |
| M1 | Providers, keyring, Store, sync, RateBudget | 3–4 |
| M2 | Daemon API, systemd, `ctl` | 1 |
| M3 | TUI MVP | 3 |
| M4 | QML UI MVP (socket spike, Service, bar, panel, window) | 3–4 |
| M5 | Notifications, presence and DND, theme and hook, `doctor` | 1.5 |
| M6 | AUR package (published only on release), `omaslack setup`, README, marketplace | 1 |
| **MVP total** | | **~13–15 weeks** |

The critical path is M1 → M2, which both clients depend on. Once M2 is stable, M3 and M4 can proceed in parallel.

## 8. Distribution

The binary ships as the AUR package `omaslack-bin` (spec `aur-packaging`), compliant with the Arch and AUR guidelines and published only by the pipeline of a release tag. The Omarchy plugin manifest cannot declare package dependencies, so the plugin checks for `omaslack-bin` at runtime and, when it is missing, offers to install it in a visible terminal with Omarchy's own `omarchy-pkg-aur-add`.

## 8b. Out of scope (v1)

Huddles and calls, Workflow Builder, Slack Connect management, app and custom emoji administration, Canvas editing, and platforms other than Omarchy.

## 9. Why OpenSpec rather than SpecKit

The project splits into **independent capabilities** (connection, sync, API, messaging, notifications, TUI, UI, integration, AUR packaging) that will evolve through incremental changes: a new provider, changes in the plugin API. OpenSpec's model fits directly:

- `specs/` as the current truth;
- `changes/` with ADDED, MODIFIED and REMOVED deltas;
- `archive` on merge.

SpecKit centers on a constitution and a linear per-feature flow, which is heavier for keeping a multi-part system maintained over time.

Status: `openspec validate add-omaslack-v1 --strict` passes with **71 requirements across 9 capabilities** (OpenSpec 2.2.0).

## 10. Next steps

1. ~~Define the plugin namespace.~~ Resolved: `ar.scabb.omaslack` (the reversed `scabb.ar` domain; free in the registry as of 2026-11-26).
2. ~~Confirm with the workspace admin which provider is allowed.~~ Resolved (2026-11-26): the admin has no objections.
3. ~~Run spike M4.1 (Quickshell `Socket` versus the `ctl --stream` bridge).~~ Resolved: a direct `Socket` works (`docs/spikes/m4.1-quickshell-socket.md`).
4. ~~Start M0.~~ Done. Next: M1.
