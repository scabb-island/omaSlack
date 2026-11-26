# Project Context — omaslack

## Purpose

omaslack is an "Omarchy Way" Slack client: lightweight, keyboard-driven and themed by the system. It offers two faces on one engine:

- **UI**: a plugin for Omarchy's Quattro shell (Quickshell/QML) with a bar icon and badges, a quick-view panel and a full window.
- **TUI**: a terminal client (bubbletea) with the same messaging features, launched with `omarchy-launch-tui`.

Both clients are thin views over a local daemon (`omaslack daemon`). The daemon is the only piece that talks to Slack, stores credentials and keeps state.

Non-negotiable design rule: **every messaging capability must be usable from both the UI and the TUI.**

**Plugin ID:** `ar.scabb.omaslack`, the reversed domain of `scabb.ar`. Marketplace IDs are permanent: it does not change.

## Tech Stack

| Piece | Technology |
|---|---|
| Daemon + CLI + TUI | Go 1.26, one `omaslack` binary with `daemon`, `tui`, `ctl`, `auth` subcommands |
| TUI | `charm.land/bubbletea/v2`, `lipgloss/v2`, `bubbles/v2` |
| Slack | `github.com/slack-go/slack` (Web API, Socket Mode) + our own WebSocket client (`gorilla/websocket`) for the browser protocol |
| Cache | SQLite via `modernc.org/sqlite` (no cgo) |
| Credentials | Secret Service (`secret-tool`/D-Bus, `godbus/dbus/v5`), falling back to a 0600 file |
| UI | QML on Quickshell (Omarchy's Quattro shell): `Quickshell`, `Quickshell.Io`, `qs.Commons`, `qs.Ui` |
| Packaging | goreleaser + PKGBUILD (AUR) for the binary; a git repo for `omarchy plugin add` |

## Project Conventions

### Language
- Everything in the repository and on GitLab is in English: code, comments, docs, specs, commit messages, merge requests and issues.

### Code Style
- Go: `gofmt`, `go vet ./...` and `golangci-lint`. Packages under `internal/` are organized by capability: `provider/`, `sync/`, `store/`, `rpc/`, `notify/`, `tui/`.
- QML: lint with `make qmllint`. Colors, fonts and spacing come only from the `Style` singleton; no hardcoded hex values.
- Any code ported from slk (MIT, © Grant Ammons) carries an attribution header in the file, and the notice is repeated in `THIRD_PARTY_NOTICES.md`.

### Architecture Patterns
- **The daemon is the single source of truth.** Clients keep no Slack state of their own: no read state, no drafts, no cache.
- **Ports and adapters.** The Slack connection is the `Provider` interface, with one implementation per connection method, and each declares which capabilities it supports.
- **Lightweight event sourcing.** The daemon publishes domain events (`message.created`, `conversation.read`, etc.) and clients update by reacting to them.

### Testing Strategy
- Go unit tests per package. Sync is tested against a fake `Provider` that replays recorded events.
- JSON-RPC contract tests: a golden request and response for every method.
- QML tests with `qmltestrunner` on the JS/QML models.
- Golden tests for the TUI (snapshot of the rendered view).

### Git Workflow
- `main` is protected: changes land only through merge requests. Every behavior change comes with its OpenSpec change (`openspec/changes/<id>`), archived on merge.

## Domain Context
- **Workspace (team):** a Slack organization. A user may have several active.
- **Conversation:** a public or private channel, a DM (`im`) or a group DM (`mpim`).
- **Thread:** replies to a root message (`thread_ts`).
- **xoxc/xoxd:** the token and cookie of the desktop app or browser session, used by the `desktop-session` provider.
- **xoxp:** a Slack app user token, used by the `internal-app` provider.
- **Internal app:** an app created and installed only in its own workspace. The 2025 rate limits for non-Marketplace apps, which cap `conversations.history` at 1 call per minute, do not apply to it.

## Important Constraints
- Omarchy plugins run **unsandboxed** inside the `omarchy-shell` process. QML therefore never handles tokens, and the heavy work (network, parsing) stays in the daemon.
- The plugin API is at `schemaVersion: 1` and young, so the UI must isolate whatever depends on the shell.
- The `desktop-session` provider uses an unofficial protocol that may violate Slack's terms of service. It is offered as an explicit option with a warning, and is never enabled by default without consent.
- The "Slack" trademark is used only to describe interoperability. It is not part of the product name or logo.

## External Dependencies
- Slack Web API, Socket Mode and the web client's WebSocket (unofficial).
- Omarchy's Quattro shell (`omarchy-shell`, `omarchy plugin`), the `~/.config/omarchy/themed/*.tpl` templates and the `~/.config/omarchy/hooks/theme-set.d/` hooks.
- Secret Service (gnome-keyring or KWallet).
