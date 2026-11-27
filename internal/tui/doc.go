// Package tui is omaslack's terminal client (spec tui-client), a thin
// client of the daemon: it holds no Slack state of its own beyond what it
// shows, and every change goes through the daemon's methods.
//
// Layout of the code (M3):
//   - client: the daemon connection the TUI uses (rpcclient in production,
//     client.Fake in tests).
//   - app: the bubbletea program: panes, modes, keys, help, the view state
//     fed by method results and events (tasks 3.1, 3.2, 3.7).
//   - render: Slack mrkdwn and message lists to styled terminal text
//     (task 3.4).
//   - theme: colors from Omarchy, reloaded on SIGUSR1 (task 3.6).
//   - image: inline images with kitty, sixel or half-block (task 3.5).
package tui
