# Tasks — add-omaslack-v1

## M0 — Skeleton and CI
- [x] 0.1 `go mod init`, `omaslack` binary with subcommands (`daemon`, `tui`, `ctl`, `auth`, `doctor`)
- [x] 0.2 golangci-lint, `go vet` and `go test` in CI (a single job, only when Go code changes). `qmllint`, `omarchy plugin validate` and OpenSpec in `make local`. goreleaser with a local snapshot and releases on tags
- [x] 0.3 `THIRD_PARTY_NOTICES.md` with slk's MIT attribution
- [x] 0.4 `plugin/` directory with a minimal `manifest.json` that passes `omarchy plugin validate`

## M1 — Connection and sync
- [ ] 1.1 `Provider` interface + `Capabilities` + normalized domain events
- [ ] 1.2 `desktop-session`: port the desktop session reader (cookies, leveldb, Secret Service) and the browser cURL login
- [ ] 1.3 `desktop-session`: real-time WebSocket with reconnection and backoff (1–30 s)
- [ ] 1.4 `internal-app`: Web API with xoxp, Socket Mode with xapp and an example app manifest
- [ ] 1.5 Keyring (Secret Service) with a 0600 fallback and a warning
- [ ] 1.6 SQLite Store (WAL) with versioned migrations
- [ ] 1.7 Sync: startup, per-conversation backfill when opened and reconciliation on reconnect
- [ ] 1.8 `RateBudget` with priorities and `Retry-After` handling
- [ ] 1.9 Tests with recorded events for both providers

## M2 — Daemon API
- [ ] 2.1 0600 socket + `SO_PEERCRED` check + JSON-RPC 2.0 over NDJSON
- [ ] 2.2 `hello` and protocol version negotiation
- [ ] 2.3 v1 methods (see design.md) with golden tests
- [ ] 2.4 `events.subscribe` with topics and `unread.summary` coalescing
- [ ] 2.5 `omaslack.service` systemd unit and `omaslack ctl` (including `--stream`)

## M3 — TUI MVP
- [ ] 3.1 Layout: workspace rail, sidebar, messages, thread and compose
- [ ] 3.2 NORMAL, INSERT and COMMAND modes, with slk-compatible keys
- [ ] 3.3 Conversation finder, search and reaction picker
- [ ] 3.4 mrkdwn rendering, mentions, emoji and day separators
- [ ] 3.5 Images with kitty, sixel or half-block
- [ ] 3.6 Theme from Omarchy's `omaslack.toml` + reload on SIGUSR1
- [ ] 3.7 Golden view tests

## M4 — QML UI MVP
- [x] 4.1 Spike: Quickshell `Socket` versus the `ctl --stream` bridge. A direct `Socket` won (see `docs/spikes/m4.1-quickshell-socket.md`)
- [ ] 4.2 `Service.qml` as the single RPC client, with its models
- [ ] 4.3 `BarWidget.qml`: icon, DM and mention badges, connection state and tooltip
- [ ] 4.4 `Panel.qml`: unread list with quick reply
- [ ] 4.5 `Window.qml` (`FloatingWindow`): sidebar, messages, threads, compose, reactions and attachments
- [ ] 4.6 Full keyboard navigation and styles only from `Style`
- [ ] 4.7 Manifest `schema[]` for settings
- [ ] 4.8 `qmltestrunner` tests for the models

## M5 — Notifications, presence and Omarchy integration
- [ ] 5.1 Notifier: rules (DM, mention, keywords, channels), DND and suppression when the conversation is in view
- [ ] 5.2 Clicking a notification sends `ui.focus` and opens the active client (window or TUI)
- [ ] 5.3 Presence and DND from both clients
- [ ] 5.4 `omaslack.toml.tpl` template + `theme-set.d/omaslack` hook
- [ ] 5.5 `.desktop` entry for the launcher (TUI) and suggested shortcuts in the README
- [ ] 5.6 `omaslack doctor`: provider, permissions, socket, theme and shell version

## M6 — Distribution
- [ ] 6.1 PKGBUILD (AUR `omaslack-bin`) + goreleaser release
- [ ] 6.2 Plugin README with install and removal instructions, `LICENSE` and `preview.png`
- [ ] 6.3 Marketplace submission (`SUBMISSION.md`), with `installation.mode: manual` if needed
- [ ] 6.4 `openspec archive add-omaslack-v1`
