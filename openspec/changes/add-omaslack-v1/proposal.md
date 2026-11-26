# Change: add-omaslack-v1 — an "Omarchy Way" Slack client with a UI and a TUI on one shared daemon

## Why

The official Slack client is an Electron app that takes 500 MB to 1.5 GB of RAM, ignores the system theme and is not built for the keyboard: the opposite of what Omarchy stands for. Alternatives exist, but each covers only part of the problem:

- **slk** (gammons/slk) is an excellent TUI, but it has no integration with the Omarchy shell.
- The five Slack plugins in the Omarchy marketplace (`janrenz.omarchy.slack`, `bottelet.slack`, `petrzpav.slack`, `leonavas.slack`, `io.github.thisisgm.slack`) each fall into one of these:
  - UI only, and constrained by the official API: 1 history call per minute and no real time.
  - Just a bar icon that drives an external TUI.
  - A single feature, such as presence and DND.

None offers a **shared engine** that feeds both a full native UI and a TUI with synchronized state. This proposal fills that gap.

## What Changes

- **ADDED** `slack-connection`: a `Provider` interface with two implementations, chosen by the user. One is `desktop-session` (the desktop app or browser session: full real time and zero setup, but unofficial). The other is `internal-app` (an internal app's user token with Socket Mode: official, without the 1-call-per-minute limit, but it needs admin approval). Includes multi-workspace support and credentials in the keyring.
- **ADDED** `sync-engine`: real time, reconnection with backoff, backfill, SQLite cache and a rate-limit budget.
- **ADDED** `daemon-api`: JSON-RPC 2.0 over a private unix socket, an event stream, protocol versioning and a systemd user unit.
- **ADDED** `messaging`: read, send, edit, delete, threads, reactions, files, mentions, search, and drafts and read state shared by both clients.
- **ADDED** `notifications-presence`: desktop notifications, rules, DND and presence.
- **ADDED** `tui-client`: a terminal client with vim-like modes, the Omarchy theme and live reload.
- **ADDED** `gui-client`: a QML plugin with a Service, a BarWidget, a quick-view Panel and a full window.
- **ADDED** `omarchy-integration`: manifest, theme template and hook, keyboard shortcuts, launcher entry and marketplace publication.
- **ADDED** `aur-packaging`: the `omaslack-bin` AUR package, compliant with the Arch and AUR guidelines and published only from tagged releases. The Omarchy plugin requires it and offers to install it.

## Impact

- **Affected specs:** the 9 capabilities above. All are new.
- **Code:** a new project. The `omaslack` repo (Go binary) and the `plugin/` directory (Omarchy plugin), published as a separate repo or as a subdirectory with `manifestPath`.
- **Users:** they choose a provider when running `omaslack auth add`. The `desktop-session` provider shows a terms-of-service warning that must be accepted.
- **Main risks:** Slack changing the unofficial protocol, changes in Omarchy's plugin API and the lack of plugin sandboxing. Details in `docs/feasibility.md` and `design.md`.
- **Attribution:** MIT code from slk (desktop auth, WebSocket client, mrkdwn rendering) is ported with its copyright notice.
