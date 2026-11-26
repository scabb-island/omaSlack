# Setting up the internal-app provider

The `internal-app` provider connects omaslack to a workspace through a Slack
app that you (or a workspace admin) create for that workspace only. It uses
public, documented Slack APIs: the Web API with your user token (`xoxp-…`) and
Socket Mode with an app-level token (`xapp-…`). Use it when the
`desktop-session` provider is not an option, for example when company policy
forbids unofficial clients.

## 1. Create the app from the manifest

1. Open <https://api.slack.com/apps> and choose **Create New App** →
   **From an app manifest**.
2. Pick the workspace you want omaslack to connect to.
3. Paste the contents of [`slack-app-manifest.yml`](slack-app-manifest.yml)
   (YAML tab), review the summary and choose **Create**.

The manifest enables Socket Mode, subscribes to message and reaction events
on your behalf and requests only the user scopes omaslack calls (listed at the
top of the file).

## 2. Create the app-level token (`xapp-…`)

1. In the app's **Basic Information** page, go to **App-Level Tokens** and
   choose **Generate Token and Scopes**.
2. Name it (for example `omaslack-socket`), add the `connections:write`
   scope and choose **Generate**.
3. Copy the token. It starts with `xapp-`.

## 3. Install the app and copy your user token (`xoxp-…`)

1. Go to **OAuth & Permissions** and choose **Install to Workspace**. If your
   workspace requires admin approval for apps, this sends a request to an
   admin; continue once it is approved.
2. Copy the **User OAuth Token**. It starts with `xoxp-`.

## 4. Register the workspace in omaslack

```bash
omaslack auth add --provider internal-app
```

Paste both tokens when asked. omaslack stores them in the Secret Service (or
in a 0600 file when there is none) and never returns them over its socket.
(`auth add` lands in milestone M1; until then it reports "not implemented".)

## What this provider can and cannot do

Supported: real-time messages, edits, deletions and reactions; history and
threads; sending; marking conversations read; and, as those features land,
search, files, presence, do not disturb and custom emoji. The manifest
already requests the user scopes for all of them, so you install the app
once and do not need to reinstall it when a feature ships.

Not available to Slack apps, so omaslack hides them for this provider:

- **Typing indicators.** Slack sends typing events only to its own clients.
- **Sidebar sections.** Slack has no public API for them; conversations are
  listed without sections.
- **Read state from other devices in real time.** The Events API has no
  "marked read" event, so a conversation you read on your phone is updated in
  omaslack the next time it syncs that conversation, not instantly.
- **Presence changes of other people in real time.** They are polled instead.

## Revoking access

Uninstall the app from the workspace (or regenerate the tokens) and run
`omaslack auth remove`. Slack then answers `token_revoked` and omaslack marks
the workspace `auth_expired`.
