# Design — add-omaslack-v1

## Context

- The goal is Slack with a UI (Omarchy's Quattro shell) and a TUI, both fully featured and sharing state.
- Constraints:
  - QML plugins run unsandboxed inside `omarchy-shell`.
  - The official API penalizes non-Marketplace apps (`conversations.history` and `conversations.replies` capped at 1 call per minute), except internal apps.
  - The desktop app protocol is unofficial.
- Stakeholders: Omarchy users with one or more workspaces, and workspace admins (when the `internal-app` provider is used).

## Goals / Non-Goals

**Goals**
- One engine (the daemon) and two clients with messaging feature parity.
- Real time with both providers.
- The window, the bar and the TUI always agree on read state, drafts and unread counts.
- Follow the Omarchy theme on both faces, with live reload.
- Under 100 MB RSS for the daemon with two active workspaces. A target, not a guarantee.

**Non-Goals (v1)**
- Huddles and calls, Workflow Builder, Slack Connect (as management), app and custom emoji administration, and Canvas editing.
- Windows and macOS. This is a product for Omarchy (Arch + Hyprland).
- Plugin sandboxing. That is up to Omarchy.

## Architecture

```
                 ┌──────────── omaslack daemon (systemd --user) ────────────┐
Slack  ◄──────►  │ Provider: desktop-session │ internal-app                 │
                 │   └─ Capabilities{realtime, search, files, presence,…}  │
                 │ Sync engine → Store (SQLite: WAL)                        │
                 │ Notifier · Presence/DND · RateBudget · Keyring           │
                 │ RPC: JSON-RPC 2.0, NDJSON, $XDG_RUNTIME_DIR/omaslack.sock│
                 └───────────────▲──────────────────────────▲───────────────┘
                                 │                          │
             omaslack tui (bubbletea v2)        QML plugin (omarchy-shell)
             omaslack ctl (scripts/hooks)       Service.qml ─ single RPC client
                                                ├ BarWidget.qml (badges)
                                                ├ Panel.qml (quick view)
                                                └ Window.qml (FloatingWindow)
```

### Single binary

`omaslack` has the subcommands `daemon`, `tui`, `ctl <method> [json]`, `auth add|list|remove` and `doctor`. One artifact keeps the AUR package simple and guarantees that client and server speak the same protocol version.

### Provider interface

```go
type Provider interface {
    Capabilities() Capabilities          // what this provider supports
    Connect(ctx) (<-chan Event, error)   // real time, normalized to the domain
    History(ctx, conv, cursor) (Page, error)
    Replies(ctx, conv, threadTS, cursor) (Page, error)
    Post / Update / Delete / React / Upload / MarkRead / Search / SetPresence / SetDND ...
}
```

- `desktop-session` uses the xoxc token and the xoxd cookie and opens the web client's WebSocket. Ported from slk: `internal/slackdesktop`, `internal/slack/connection.go` and `events.go`.
- `internal-app` uses the xoxp user token over the Web API (slack-go) and receives events through Socket Mode with an xapp app token. Whatever a provider does not support is declared in `Capabilities`, and clients hide or disable that action.

### Store

SQLite at `$XDG_DATA_HOME/omaslack/<workspace>.db`, WAL mode. Main tables:

| Table | Key | Notes |
|---|---|---|
| `workspaces` | team_id | name, url, provider, connection state |
| `users` | (team_id, user_id) | profile, avatar_path, presence |
| `conversations` | (team_id, conv_id) | type, name, section, mute, `last_read`, `unread_count`, `mention_count` |
| `messages` | (team_id, conv_id, ts) | `thread_ts`, author, mrkdwn text, `blocks_json`, `edited_ts`, `deleted` |
| `reactions` | (team_id, conv_id, ts, emoji, user) | |
| `files` | (team_id, file_id) | metadata and cached `local_path` |
| `drafts` | (team_id, conv_id, thread_ts) | **shared by the TUI and the UI** |
| `kv` | key | sync cursors and emoji frecency |

Slack remains the remote source of truth. The Store is a cache that can be rebuilt.

### Local protocol (daemon-api)

- Transport: unix socket `$XDG_RUNTIME_DIR/omaslack.sock` with 0600 permissions, NDJSON framing (one JSON object per line) and JSON-RPC 2.0.
- Handshake: `hello{client, protocol: 1}` returns `{protocol, daemonVersion, workspaces[], capabilities}`. A major version mismatch gets error `-32001 incompatible_protocol`.
- Methods (v1):
  - `workspaces.list`.
  - `conversations.list{team, filter}`, `conversations.open{team, conv}`, `conversations.close{team, conv}` (the client left it, so the engine stops treating it as open), `conversations.markRead{team, conv, ts}`.
  - `messages.history{team, conv, before?, limit}`, `messages.replies{…, threadTS}`, `messages.send{team, conv, threadTS?, text, files?, broadcast?}`, `messages.edit` and `messages.delete`.
  - `reactions.add` and `reactions.remove`.
  - `files.upload{team, conv, paths[], caption}` and `files.fetch{team, fileId}` (returns `local_path`).
  - `drafts.get` and `drafts.set`.
  - `search.messages{team, query}`, `users.search{team, q}`.
  - `presence.set{team, auto|away}`, `dnd.snooze{team, minutes}`, `dnd.end`.
  - `unread.summary{}`: the aggregate the bar uses.
  - `ui.focus{team, conv, ts?}`: asks a client to open a conversation, for deep links and notification clicks.
- Events: `events.subscribe{topics[]}` turns the connection into a stream of JSON-RPC notifications. Topics:
  - `message.created|updated|deleted`, `reaction.changed`, `conversation.updated`.
  - `read.changed`, `draft.changed`, `typing`, `presence.changed`.
  - `connection.state`, `unread.summary`, `ui.focus`.
- Coalescing: `unread.summary` goes out at most once every 250 ms, so the bar is not flooded.
- Concurrency: at most 16 calls run at once per connection. Past that the daemon stops reading the connection until one finishes (backpressure, no error).
- Slow subscribers: each subscriber has a bounded queue (1024 events). A subscriber that overflows it is disconnected and logged, and reloads on reconnect, instead of silently missing events.
- Shapes: method results and event payloads are the types in `internal/wire`.

### Clients

- **TUI**: just another RPC client. It can start the daemon if it is not running (`systemctl --user start omaslack`, or a child process if that fails). Keys match slk where it makes sense, so slk users have nothing to relearn.
- **QML plugin**:
  - `Service.qml` (`kinds: service`) keeps **a single** connection to the socket through `Quickshell.Io` `Socket` (validated by spike M4.1).
  - It exposes models (`ListModel`) to `BarWidget`, `Panel` and `Window`.
  - This avoids bug SLACK-1 in `janrenz.omarchy.slack` (GAPS.md), where every surface had its own Service and read state drifted.

### Theming

- **UI**: uses only `Style` (shell colors and fonts), so it follows the Omarchy theme with no extra work.
- **TUI**:
  - The `~/.config/omarchy/themed/omaslack.toml.tpl` template is installed, with `{{ background_strip }}`, `{{ foreground_strip }}`, `{{ blue_strip }}`, etc.
  - Omarchy renders it to `~/.local/state/omarchy/current/theme/omaslack.toml`.
  - The `~/.config/omarchy/hooks/theme-set.d/omaslack` hook sends `SIGUSR1` to `omaslack tui`, as slk does in `themereload_unix.go`.
  - Without the template, the TUI reads the active theme's `colors.toml`.

### Packaging and the plugin dependency

- The binary ships as the AUR package `omaslack-bin`, generated by goreleaser's `aurs` publisher from the release tarballs. It is published **only** by the release pipeline of a `vX.Y.Z` tag; the AUR SSH key is a protected, masked variable that no branch or MR pipeline can read.
- The package installs only system files (binary, systemd user unit, `.desktop`, licenses, and the theme template and hook under `/usr/share/omaslack/`). Per-user setup is `omaslack setup`, because a package must not write to `$HOME`.
- The Omarchy manifest has no dependency field and `omarchy plugin add` runs no hooks (checked against `omarchy-plugin-validate` and `omarchy-plugin-add`). The plugin therefore checks for the package at runtime with `omarchy-pkg-present` and, when it is missing or its protocol is incompatible, offers to install or update it in a visible terminal with `omarchy-pkg-aur-add omaslack-bin`, the same helpers Omarchy itself uses.

### Security

- Tokens are stored only in the Secret Service, with the attribute `service=omaslack`. Without a Secret Service they go to `$XDG_CONFIG_HOME/omaslack/credentials` with 0600 permissions and a visible warning.
- No RPC method returns tokens. `doctor` shows them masked.
- The socket is 0600 in `$XDG_RUNTIME_DIR`, so only its owner can use it. `SO_PEERCRED` is checked as well.
- Downloaded files go to `$XDG_CACHE_HOME/omaslack/files`, with sanitized names.
- Links always open through `xdg-open`, and message content is never executed.

## Decisions

| Decision | Chosen | Rejected alternatives and why |
|---|---|---|
| Codebase | From scratch, porting MIT pieces of slk | **Fork slk**: about 107k lines coupled to bubbletea (events are `tea.Msg`); splitting them is nearly a rewrite, and the fork would drift from upstream. **slk untouched + companion**: the UI would not be a full client and would depend on slk's internal cache schema. |
| Connection | Both, chosen by the user | Official only: unusable without an internal app (1 call per minute). Desktop only: terms-of-service risk with no alternative. |
| Daemon language | Go | Python (like janrenz and petrzpav): slower for WebSocket, rendering and SQLite, no single binary, and no slk code to port. Rust: viable, but the desktop login and protocol would be written from scratch, which lengthens M1. C++/Qt: shares the UI toolkit but does not fit the TUI. |
| IPC | JSON-RPC 2.0 + NDJSON over a unix socket | D-Bus: Quickshell has no convenient generic D-Bus client in QML, and it is heavier. Local HTTP: needless attack surface. |
| Full window | Quickshell `FloatingWindow` inside the plugin | A separate Qt app, like the omarchy-app skill: more work, and the requirement is a plugin. Kept as a possible evolution. |
| Specs | OpenSpec | SpecKit: its constitution and per-feature flow fit capabilities that evolve through deltas less well. |

## Risks / Trade-offs

| Risk | Prob. | Impact | Mitigation |
|---|---|---|---|
| Slack changes the web client protocol | Medium | High | Isolated `Provider` and tests with recorded events. `internal-app` is plan B and switching is one command (`auth add` + `auth remove`). |
| Terms of service or corporate policy | Medium | High | `desktop-session` only with explicit consent and a warning in `doctor`. The internal-app path is documented for admins. |
| Web API rate limits | High for non-internal apps | Medium | `RateBudget` per method and tier, priorities (user requests before backfill) and honoring `Retry-After`. |
| Omarchy plugin API changes (`schemaVersion` 1) | Medium | Medium | Keep the QML surface small. Logic lives in the daemon. `omarchy plugin validate` and `qmllint` against the current shell in `make local`. |
| QML performance with long lists | Medium | Medium | Virtualized `ListView`, 50-message pages and mrkdwn rendered to rich text by the daemon. |
| Images in the TUI | High (Alacritty has no graphics) | Low | kitty or sixel depending on the terminal, half-block fallback (like slk) and an option to open the image in `imv`. |
| Unsandboxed plugin | Low | High | QML never handles tokens or downloads Slack content: it only asks the daemon for a `local_path`. |

## Migration Plan

Not applicable: this is a new project. For slk users, `omaslack auth add --from-slk` may import the workspaces they already configured in slk, reading `~/.config/slk/`. Optional, after the MVP.

## Open Questions

1. ~~Does Quickshell's `Socket` deliver line reads reliably on a long stream?~~ **Resolved (2026-11-26):** yes. `Socket` + `SplitParser` carried 100,000 events and 16 MB without losses, with a worst event-loop pause of 33 ms. The `ctl --stream` bridge is not needed. Connection retries need backoff so they do not flood the log. Details in `docs/spikes/m4.1-quickshell-socket.md`.
2. Does the shell's notification service (`omarchy.notifications`) support actions with callbacks? If not, use `notify-send --action` and deliver the click through `ui.focus`.
3. Should the plugin live in its own repo or as a `manifestPath` inside the monorepo? It depends on what the marketplace accepts for sources with an external binary (`installation.mode: manual`).
