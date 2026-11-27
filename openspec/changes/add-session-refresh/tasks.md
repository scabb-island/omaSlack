# Tasks — add-session-refresh

## Capa 1 — CLI + daemon reload
- [x] 1.1 `auth`: re-read a session for a given team from its local sources (desktop app + browser profiles), return fresh DesktopSession creds or a not-found error
- [x] 1.2 `daemon`/`api`: `workspaces.reload` method triggers the existing reload; `ctl` can call it
- [x] 1.3 `cli`: `omaslack auth refresh [team-id]` — re-read source, validate with auth.test, write keyring, call workspaces.reload; no consent/menu; internal-app skipped; with no arg, refresh the auth_expired desktop-session workspaces (from workspaces.list)
- [x] 1.4 `cli`/`auth` tests, including the "source no longer valid" help

## Close-out

- [x] 3.1 Live test: `auth refresh <team>` re-reads the source, validates and stores; reload RPC covered by a daemon unit test
- [x] 3.2 make local green; MR
