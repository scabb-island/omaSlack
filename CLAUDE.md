# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project status

omaslack is an "Omarchy Way" Slack client with a UI (a QML plugin for Omarchy's Quattro shell) and a TUI (Go/bubbletea) on one shared daemon. M0 (skeleton and CI) is done and spike M4.1 is resolved. The next milestone is M1 (providers and sync). Every subcommand except `version` still returns "not implemented" (exit 3) and names the milestone it lands in.

- Feasibility report: `docs/feasibility.md`. Spike results: `docs/spikes/`
- Context, stack and conventions: `openspec/project.md`
- Change in progress: `openspec/changes/add-omaslack-v1/` (`tasks.md` tracks progress per milestone)
- The Omarchy plugin ID is `ar.scabb.omaslack`. It is permanent in the marketplace, so it does not change.
- Repo: gitlab.com/scabb-island/omaslack (private)

## Language

Everything in this repository and on GitLab is written in English: code, comments, docs, specs, commit messages, merge requests and issues. The only exception is test data that deliberately exercises non-ASCII text.

## Toolchain and commands

Tools are pinned in `mise.toml`: Go 1.26, golangci-lint 2.14.0 and goreleaser 2.20.2. Inside the directory they already resolve to those versions. From scripts, use `mise exec -- <cmd>`.

```bash
make check            # vet + golangci-lint + test: exactly what CI runs
make local            # check + test -race + qmllint + omarchy plugin validate + OpenSpec
make build            # bin/omaslack with the version injected through ldflags
make snapshot         # goreleaser snapshot (linux amd64/arm64) in dist/
go test ./internal/cli -run TestVersion   # a single test
```

For QML, `make qmllint` maps `$OMARCHY_PATH/shell` as the `qs` import prefix (the way the shell does) and turns off `missing-property`, because the shell hands plugins a `bar` object with no static type.

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
