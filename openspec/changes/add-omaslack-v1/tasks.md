# Tasks — add-omaslack-v1

## M0 — Skeleton and CI
- [x] 0.1 `go mod init`, `omaslack` binary with subcommands (`daemon`, `tui`, `ctl`, `auth`, `doctor`)
- [x] 0.2 golangci-lint, `go vet` and `go test` in CI (a single job, only when Go code changes). `qmllint`, `omarchy plugin validate` and OpenSpec in `make local`. goreleaser with a local snapshot and releases on tags
- [x] 0.3 `THIRD_PARTY_NOTICES.md` with slk's MIT attribution
- [x] 0.4 `plugin/` directory with a minimal `manifest.json` that passes `omarchy plugin validate`

## M1 — Connection and sync
- [x] 1.1 `Provider` interface + `Capabilities` + normalized domain events (`internal/domain`, `internal/provider`)
- [x] 1.2 `desktop-session`: port the desktop session reader (cookies, leveldb, Secret Service) and the browser cURL login (`internal/provider/desktopsession/auth`)
- [x] 1.3 `desktop-session`: real-time WebSocket with reconnection and backoff (1–30 s)
- [x] 1.4 `internal-app`: Web API with xoxp, Socket Mode with xapp and an example app manifest (`internal/provider/internalapp`, `docs/slack-app-manifest.yml`)
- [x] 1.5 Keyring (Secret Service) with a 0600 fallback and a warning (`internal/keyring`)
- [x] 1.6 SQLite Store (WAL) with versioned migrations (`internal/store/sqlite`)
- [x] 1.7 Sync: startup, per-conversation backfill when opened and reconciliation on reconnect. The internal-app provider gets no real-time read markers, presence or typing (RTM-only), so the sync reconciles read markers by polling
- [x] 1.8 `RateBudget` with priorities and `Retry-After` handling (`internal/ratebudget`)
- [x] 1.9 Tests with recorded events for both providers
- [x] 1.10 Store: add `Replies`, and make `Messages` return top-level messages and thread roots only (before 1.7)
- [x] 1.11 Domain: a bot author on `domain.Message`, so providers stop dropping `bot_message` messages (before 1.7)
- [x] 1.12 Internal-app manifest: request the full scope set the declared capabilities need (`search:read`, `files:read`, `files:write`, `users:write`, `emoji:read`)
- [x] 1.13 RateBudget: a 429 also drains the accumulated burst, so calls do not burst again the moment the penalty ends
- [x] 1.14 Desktop login: list desktop workspaces without a token as "needs sign-in" instead of skipping them
- [x] 1.15 `omaslack auth add|list|remove` CLI, including the terminal paste reader ported from slk
- [x] 1.16 `Provider.Conversations` in both providers; the sync engine uses it to catch up every conversation with unread messages or mentions after an outage and to seed unread counts on a cold start
- [x] 1.17 internal-app implements `provider.ReadMarkerReader` with `conversations.info`
- [x] 1.18 desktop-session implements `provider.PresenceSubscriber` and `provider.TypingSender` over the web client socket
- [x] 1.19 Tolerant reaction mapping: accept reactions without `item.type` (as slk does) and log unexpected item types
- [x] 1.20 The sync engine's `LoadOlder` continues from `Store.Oldest` instead of a client-supplied cursor
- [x] 1.21 `auth add`: picking a workspace that needs sign-in goes straight to the browser paste flow
- [x] 1.22 desktop-session: when only `client.counts` fails (Enterprise Grid), return the conversation list without read state
- [x] 1.23 desktop-session: remember the presence subscription and send it again on every socket that opens
- [x] 1.24 Sync engine: log the conversation list failing and recovering when the state changes
- Deferred: honoring a deliberate "mark unread" from another Slack client (needs an explicit signal on `ReadChanged`)

## M2 — Daemon API
- [x] 2.1 0600 socket + `SO_PEERCRED` check + JSON-RPC 2.0 over NDJSON (`internal/rpc`)
- [x] 2.2 `hello` and protocol version negotiation
- [ ] 2.3 v1 methods (see design.md) with golden tests, plus a cap on concurrent calls per connection
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
- [ ] 6.1 `omaslack-bin` PKGBUILD generated by goreleaser (`aurs`), with the file layout of spec `aur-packaging`
- [ ] 6.1a `omaslack setup` for per-user setup (theme template, hook, user service), so the package never writes to `$HOME`
- [ ] 6.1b `make aur-check`: namcap, `.SRCINFO` and a clean build in `archlinux:base-devel` (local only)
- [ ] 6.1c Release job publishes to the AUR only on `vX.Y.Z` tags, with the SSH key in a protected, masked variable
- [ ] 6.1d Plugin: detect `omaslack-bin` with `omarchy-pkg-present`, check the protocol, and offer install/update through `omarchy-launch-floating-terminal-with-presentation omarchy-pkg-aur-add omaslack-bin`
- [ ] 6.2 Plugin README with install and removal instructions, `LICENSE` and `preview.png`
- [ ] 6.3 Marketplace submission (`SUBMISSION.md`), with `installation.mode: manual` if needed
- [ ] 6.4 `openspec archive add-omaslack-v1`
