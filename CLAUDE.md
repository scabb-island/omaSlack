# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project status

omaslack is an "Omarchy Way" Slack client with a UI (a QML plugin for Omarchy's Quattro shell) and a TUI (Go/bubbletea) on one shared daemon. M0, M1 and M2 are done: the domain model, both providers (`internal/provider/internalapp`, `internal/provider/desktopsession`, sharing `internal/provider/slackwire`), the keyring, the SQLite store, the rate budget, the sync engine (`internal/syncengine`), `omaslack auth add|list|remove`, and the daemon API: `omaslack daemon` (`internal/daemon`) serves the v1 methods (`internal/api`, JSON shapes in `internal/wire`) and events (`internal/events`) on the socket, and `omaslack ctl` (`internal/rpcclient`) calls it. M3 is done too: `omaslack tui` (`internal/tui/app`, view state in `app.State`, golden views in `internal/tui/app/testdata`, rewritten with `go test ./internal/tui/app -update`) renders mrkdwn (`internal/tui/render`), follows the Omarchy theme (`internal/tui/theme`), shows inline images (`internal/tui/image`) and has the finder, search, reactions, autocomplete and edit/delete/reply. Next: M4 (the QML plugin). `doctor` still returns "not implemented" (exit 3) and names the milestone it lands in.

- Feasibility report: `docs/feasibility.md`. Spike results: `docs/spikes/`
- Context, stack and conventions: `openspec/project.md`
- Change in progress: `openspec/changes/add-omaslack-v1/` (`tasks.md` tracks progress per milestone)
- The Omarchy plugin ID is `ar.scabb.omaslack`. It is permanent in the marketplace, so it does not change.
- Repo: gitlab.com/scabb-island/omaslack (private)

## Rule: English only

omaslack is meant for an international audience. **Everything you write for this project MUST be in English**, even when the conversation with the user is in another language:

- Code: identifiers, comments, log messages, errors and user-facing strings (CLI output, QML tooltips and labels).
- Docs and specs: `README`s, `docs/`, `openspec/` (proposals, designs, tasks, specs) and this file.
- Git: commit messages, branch names and tags.
- GitLab: merge request titles and descriptions, review comments, issues, and project settings such as the description.
- Published material about the project, such as the feasibility report page.

The only exception is test data that deliberately exercises non-ASCII text (accents, emoji). Keep it, and say in a comment that it is intentional.

## Toolchain and commands

Tools are pinned in `mise.toml`: Go 1.26, golangci-lint 2.14.0, goreleaser 2.20.2 and the ub-mode quality tools. On a fresh clone, run `mise install`. Inside the directory they already resolve to those versions. From scripts, use `mise exec -- <cmd>`.

```bash
make check            # vet + golangci-lint + test: exactly what CI runs
make local            # check + test -race + qmllint + omarchy plugin validate + OpenSpec
make build            # bin/omaslack with the version injected through ldflags
make snapshot         # goreleaser snapshot (linux amd64/arm64) in dist/
go test ./internal/cli -run TestVersion   # a single test
```

For QML, `make qmltest` runs the plugin tests: `qmltestrunner` on `plugin/tests/tst_*.qml` (the pure JS in `plugin/lib/` and `FakeService.qml`), then `plugin/tests/smoke/run.sh`, which loads each `plugin/tests/smoke/*Smoke.qml` in an offscreen `quickshell` against the installed shell, because the views import Quickshell and `qs.Ui`, which `qmltestrunner` cannot load. Popups built on `KeyboardPanel` need Wayland, so smoke their content, not the popup. `make qmllint` maps `$OMARCHY_PATH/shell` as the `qs` import prefix (the way the shell does) and turns off `missing-property`, because the shell hands plugins a `bar` object with no static type.

## ub-mode (change discipline)

The project includes the `ub-mode` skill (`.claude/skills/ub-mode/SKILL.md`, from github.com/unclebob/ub-mode, MIT): make it work first (with TDD), then make it right, in small commits. Its quality gates run locally with unclebob's crapper, dryer and mutator. mise installs them with the `pypi` backend, from git, pinned by commit in `mise.toml`. mutator carries crapper in its own environment (`uvx_args = "--with ..."`) because that is where it reads function names from. If you bump crapper, keep the same commit in both entries.

```bash
make crap      # crapper: CRAP per function, fails if any reaches 10
make dry       # dryer: structural duplication >= 0.8
make mutate    # mutator: mutation testing; differential from the second run on
make quality   # all three
```

They write their snapshots to `.metrics/` (gitignored) and never run in CI. uml-viewer is not installed: it needs Clojure and Java 21, and the skill does not use it.

## CI (GitLab): keep it cheap

CI minutes are limited. Keep `.gitlab-ci.yml` like this:

- A single `check` job on the `golang:1.26-alpine` image. golangci-lint is downloaded as a binary with a pinned checksum; it is not compiled.
- It runs only when Go code or the CI config changes. Docs and spec changes cost no minutes.
- No duplicate branch and MR pipelines.
- Only `main` writes the cache; MRs only read it.
- `release` runs only on `vX.Y.Z` tags.
- Jobs run on the group's own Kubernetes runner; shared runners are disabled for the project.

Anything that needs an Omarchy machine (qmllint against the shell, `omarchy plugin validate`, OpenSpec) belongs in `make local`, never in CI. Before changing CI, simulate the job locally with `podman run ... golang:1.26-alpine` instead of testing with pushes.

`main` is protected: nobody pushes to it directly, and changes land only through merge requests.

## OpenSpec workflow

Every behavior change starts as an OpenSpec change before any code is touched, and is archived on merge.

```bash
npx -y @fission-ai/openspec@latest show add-omaslack-v1
npx -y @fission-ai/openspec@latest validate --all --strict      # also run by `make specs`
npx -y @fission-ai/openspec@latest archive add-omaslack-v1      # once tasks.md is complete
```

Spec format: `## ADDED Requirements`, `### Requirement:`, `#### Scenario:`, **SHALL/MUST** in the requirement text, and `- **WHEN**` / `- **THEN**` bullets. Every requirement needs at least one scenario.

## Architecture (decisions already made)

- **Single binary:** `cmd/omaslack` dispatches subcommands through `internal/cli`, whose dependencies are injected with `cli.Env` so tests never touch stdout. The protocol version lives in `internal/version.Protocol`.
- **The daemon is the single local source of truth.** It is the only thing that talks to Slack and stores credentials. The TUI and the QML plugin are thin clients over JSON-RPC 2.0 + NDJSON on `$XDG_RUNTIME_DIR/omaslack.sock`. Do not add Slack state to the clients: that is how the "the bar never learns the window marked it read" bug (SLACK-1 in the janrenz plugin) is avoided.
- **Plugin (`plugin/`):** the shell mounts `Service.qml` once and each bar gets it with `bar.shell.serviceFor("ar.scabb.omaslack")`. The Service connects straight to the socket with `Quickshell.Io` `Socket` + `SplitParser`, validated in spike M4.1. Connection retries use backoff, because every failure leaves a WARN in the shell log.
- **QML runs unsandboxed** inside `omarchy-shell`: no tokens and no Slack requests from QML. Colors and fonts come only from `Style` and `Commons`.
- **Two connection providers** behind `Provider`: `desktop-session` (xoxc/xoxd, unofficial, requires explicit consent) and `internal-app` (xoxp + Socket Mode). Whatever a provider does not support is declared in `Capabilities`.
- **Built from scratch, not a fork of slk.** MIT code from `gammons/slk` is ported with an attribution header in each file and in `THIRD_PARTY_NOTICES.md`.
- **UI/TUI parity:** every messaging operation must exist in both clients.
- **`spikes/`** holds throwaway experiments run by hand. golangci-lint skips gosec and noctx there.
