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
- [x] 1.22 desktop-session: when only `client.counts` fails (workspaces where `client.counts` is refused), return the conversation list without read state
- [x] 1.23 desktop-session: remember the presence subscription and send it again on every socket that opens
- [x] 1.24 Sync engine: log the conversation list failing and recovering when the state changes
- [x] 1.25 `auth add`: check desktop app credentials with `auth.test` before storing them, explain a session Slack rejects (point to `internal-app`), and require an interactive terminal
- Deferred: honoring a deliberate "mark unread" from another Slack client (needs an explicit signal on `ReadChanged`)

## M2 — Daemon API
- [x] 2.1 0600 socket + `SO_PEERCRED` check + JSON-RPC 2.0 over NDJSON (`internal/rpc`)
- [x] 2.2 `hello` and protocol version negotiation
- [x] 2.3 v1 methods (see design.md) with golden tests, plus a cap on concurrent calls per connection (`internal/api`, shapes in `internal/wire`)
- [x] 2.4 `events.subscribe` with topics and `unread.summary` coalescing (`internal/events`)
- [x] 2.5 `omaslack.service` systemd unit and `omaslack ctl` (including `--stream`)
- [x] 2.6 Provider writes in both providers, as optional interfaces: edit, delete, reactions, message and user search, presence, DND
- [x] 2.7 Store keeps the conversation list (kind, name, latest, mentions); the sync engine stores it and runs every client action (send, edit, delete, react, mark read, drafts) as the single writer
- [x] 2.8 `omaslack daemon`: one engine per signed-in workspace from the keyring, the hub and the methods on the socket, hello with workspaces and capabilities
- [x] 2.9 `files.upload` and `files.fetch`
- [x] 2.10 Read-state fill for a provider whose list lacks it (internal-app): `conversations.info` per conversation (plus a one-message `conversations.history` when it names no latest message), at background priority, DMs first, dormant group DMs skipped, one pass at a time and at most every 30 minutes
- [x] 2.11 Per-subscriber queues coalesce state events by key, and the slow-subscriber bound counts only message, reaction and focus events, so a reconcile burst on a large workspace no longer disconnects a client that reads (`internal/events`)
- [x] 2.12 desktop-session sends every session cookie (from a pasted cURL or the desktop app's Cookies database), not only `d`, and calls the Web API on the workspace's own domain; `auth add` stores the `auth.test` URL with the credentials of both providers (internal-app keeps calling `slack.com`), and entries stored before keep working with `d` alone
- [x] 2.13 Daemon fixes from a live run: a coalesced `unread.summary` is only sent when it differs from the last one that subscriber got, and the sync engine's log lines name the team once
- [x] 2.14 desktop-session socket declares a client build (`agent_version` in `start_args`); without it Slack accepts the socket but never says hello. Error frames and a missing hello are logged
- Deferred:
  - Realtime mention counting (mentions refresh with the conversation list and the read-marker poll).
  - Unread thread counts (need thread read markers).
  - When `chat.update` omits `edited.ts`, Edited is the local clock and the later, older Slack value is not stored (the text is right; only the edit time can be off).
  - The systemd unit is not shipped yet: `.goreleaser.yaml` must install `packaging/systemd/omaslack.service` at `/usr/lib/systemd/user/` (task 6.1).
- Client notes for M3/M4: a sent message arrives twice (the action and Slack's echo) with the same content, so clients key messages by conversation + ts; a workspace whose `state` is `auth_expired` needs `omaslack auth add` and `systemctl --user reload omaslack`; `files.fetch` paths are local to this machine.

## M3 — TUI MVP
- [x] 3.1 Layout: workspace rail, sidebar, messages, thread and compose
- [x] 3.2 NORMAL, INSERT and COMMAND modes, with slk-compatible keys
- [x] 3.3 Conversation finder, search and reaction picker
  - Ctrl+k/Ctrl+t: fuzzy finder over the conversations of every workspace (`rankConversations`: match kind first, then unread/mentions, then the newest). It opens existing conversations only; opening new DMs and joining channels needs daemon methods v1 does not have. `/` or `:search <query>` runs `search.messages`; a hit opens its conversation with the message selected, asking `messages.history{before: ts + 1µs}` until the hit arrives (the daemon loads one older page per call, so up to 10 calls); a `messages.around`-style method would make that one call. `r` or `:react <name>` toggles a reaction; it removes only once `Directory.Self` is known, until then it always adds. Recent emoji are remembered per session, not across runs.
- [x] 3.4 mrkdwn rendering, mentions, emoji and day separators
  - Notes for 3.1 and 3.5: the app's `render.Directory` must resolve bot (`B…`) IDs in `UserName` too, and `Emoji` only for custom emoji (standard ones come from `render.StandardEmoji`). Image file rows carry an `omaslack-image:<id>` OSC 8 link on their `[image]` tag; `render.ImageFileID(line)` finds them so 3.5 can draw the image below. Thread roots show "↳ N replies" from `replyCount` (3.9), and "↳ thread" while the count is unknown. `Messages` takes about 20 ms for 1000 messages, so render on changes, not on every frame.
- [x] 3.5 Images with kitty, sixel or half-block
  - `internal/tui/image`: `Detect` (env rules, `OMASLACK_IMAGES` override), `Size` and `Render` (max 50 MP, PNG/JPEG/GIF/WebP). Rendering is CPU-bound: the app runs it in a `tea.Cmd` and prints kitty/sixel output outside the cell renderer.
  - App (`internal/tui/app/images.go`, `files.go`): pictures load lazily (View notes visible image rows, a tick 50 ms later calls `files.fetch` and renders) and are cached by path, protocol and room, at most 12 rows. Kitty/sixel: View leaves blank rows marked with an invisible OSC 8 link, and the tick prints the picture with `tea.Raw` (save cursor, CUP, sequence, restore) after the frame, sending `ClearKitty` first when placements change. `I` toggles inline images: on by default for kitty and half-block, off for sixel, whose pixels can linger in cells bubbletea thinks unchanged. `o` opens a message's file with `xdg-open` (a chooser when it has several); `:attach <path> [caption]` (Ctrl+o in INSERT) uploads to the open conversation or focused thread. Both need the workspace's `files` capability. Not yet: pasting a clipboard image, and the compose text as the caption.
- [x] 3.6 Theme from Omarchy's `omaslack.toml` + reload on SIGUSR1
  - `internal/tui/theme`: `Load` (omaslack.toml, else colors.toml through `roles.go`) and `Watch`. The app wires `signal.Notify(SIGUSR1)` into `Watch`.
- [x] 3.7 Golden view tests
- [x] 3.8 Daemon methods the clients need to show names: `users.info{team, ids[]}`, the other person of each DM in `conversations.list` (`user`), `emoji.list{team}` and `typing.send{team, conv}`
- [x] 3.9 Who is signed in and thread reply counts: `self` in each workspace of `hello` and `workspaces.list` (the TUI's `Directory.Self`), and `replyCount`/`latestReply` on thread roots, kept current as replies arrive in real time, so the TUI shows "N replies"

## M4 — QML UI MVP
- [x] 4.1 Spike: Quickshell `Socket` versus the `ctl --stream` bridge. A direct `Socket` won (see `docs/spikes/m4.1-quickshell-socket.md`)
- [ ] 4.2 `Service.qml` as the single RPC client, with its models
- [x] 4.3 `BarWidget.qml`: icon, DM and mention badges, connection state and tooltip
- [x] 4.4 `Panel.qml`: unread list with quick reply
- [ ] 4.5 `Window.qml` (`FloatingWindow`): sidebar, messages, threads, compose, reactions and attachments
- [ ] 4.6 Full keyboard navigation and styles only from `Style`
- [x] 4.7 Manifest `schema[]` for settings
- [ ] 4.8 `qmltestrunner` tests for the models
- [ ] 4.9 Daemon-side rich text: mrkdwn parsed once in a shared `internal/mrkdwn` package (also used by the TUI renderer) and rendered to the HTML subset Qt's `Text.RichText` draws, in `wire.Message.html` for history, replies and message events
- [x] 4.10 Contracts: the Service API (`plugin/Service.qml`, mirrored by `plugin/tests/FakeService.qml`), pure JS state in `plugin/lib/`, the bar/popup/window split (`BarWidget.qml`, `BarIcon.qml`, `QuickPanel.qml`, `Window.qml` as the `panel` entry point), the manifest settings, `wire.Message.html`, and `make qmltest` (qmltestrunner for JS and FakeService; view smokes in an offscreen quickshell)

## M5 — Notifications, presence and Omarchy integration
- [ ] 5.1 Notifier: rules (DM, mention, keywords, channels), DND and suppression when the conversation is in view
- [ ] 5.2 Clicking a notification sends `ui.focus` and opens the active client (window or TUI)
- [ ] 5.3 Presence and DND from both clients
- [ ] 5.4 `omaslack.toml.tpl` template + `theme-set.d/omaslack` hook
  - The files exist in `packaging/omarchy/` (task 3.6); installing them into `$HOME` is 6.1a.
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
