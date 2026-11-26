## ADDED Requirements

### Requirement: Private local socket
The daemon SHALL listen on a unix socket at `$XDG_RUNTIME_DIR/omaslack.sock` with 0600 permissions and SHALL reject connections whose UID (from `SO_PEERCRED`) is not the daemon owner's.

#### Scenario: Another user tries to connect
- **WHEN** a process with a different UID connects to the socket
- **THEN** the daemon closes the connection without processing any message and logs it

### Requirement: JSON-RPC 2.0 over NDJSON
The protocol SHALL be JSON-RPC 2.0 with one JSON object per line (NDJSON). Errors SHALL use the standard JSON-RPC codes plus custom codes from `-32000` (for example `-32001 incompatible_protocol`, `-32002 unsupported_capability`, `-32003 auth_expired`).

#### Scenario: Unknown method
- **WHEN** a client sends `{"jsonrpc":"2.0","id":1,"method":"foo.bar"}`
- **THEN** the daemon answers with error `-32601 Method not found` and the connection stays open

### Requirement: No batch requests
The daemon SHALL reject JSON-RPC batch requests (a JSON array on one line) with error `-32600` and keep the connection open. Clients send one request per line.

#### Scenario: A client sends a batch
- **WHEN** a client sends `[{"jsonrpc":"2.0","id":1,"method":"workspaces.list"}]`
- **THEN** the daemon answers with error `-32600 Invalid Request` and the connection stays open

### Requirement: Bounded concurrency per connection
The daemon SHALL run at most 16 calls at once per connection. While 16 are running it SHALL stop reading that connection until one finishes, so a client cannot make the daemon queue unbounded work. Other connections SHALL NOT be affected.

#### Scenario: A client floods calls
- **WHEN** a client sends 100 slow calls without waiting for answers
- **THEN** at most 16 of them run at the same time, all 100 are eventually answered, and another client's calls are answered meanwhile

### Requirement: Slow subscribers are disconnected
Publishing an event SHALL NOT block the sync engine. Each subscriber SHALL have a bounded queue; a subscriber whose queue overflows SHALL be disconnected and the daemon SHALL log it, so that a client never shows a view with silently missing events.

#### Scenario: A client stops reading
- **WHEN** a subscribed client stops reading its socket and more than 1024 events are queued for it
- **THEN** the daemon closes that connection, logs it, and keeps delivering events to every other subscriber

### Requirement: Handshake and protocol version
Every client SHALL start with `hello{client, protocol}`. The daemon SHALL answer with its protocol version, daemon version, workspaces and capabilities. If the major protocol version does not match, it SHALL answer `-32001` and close the connection.

#### Scenario: QML plugin newer than the daemon
- **WHEN** the plugin sends `hello{protocol: 2}` and the daemon only supports version 1
- **THEN** the daemon answers `-32001 incompatible_protocol` and the bar shows "update omaslack"

### Requirement: Event subscription
A client SHALL be able to call `events.subscribe{topics[]}` and from then on receive JSON-RPC notifications (without `id`) for the requested topics on the same connection. `unread.summary` SHALL be coalesced to at most one emission every 250 ms.

#### Scenario: Message burst
- **WHEN** 40 messages arrive within one second
- **THEN** each client subscribed to `message.created` receives 40 events, and a client subscribed to `unread.summary` receives at most 4

### Requirement: Messaging and state methods
The daemon SHALL expose at least the v1 methods listed in `design.md`: workspaces, conversations, messages, reactions, files, drafts, search, users, presence, dnd, unread.summary and ui.focus. A method that needs a capability the provider lacks SHALL answer `-32002 unsupported_capability`.

#### Scenario: Search not supported
- **WHEN** a client calls `search.messages` on a workspace whose provider declares `search = false`
- **THEN** the daemon answers `-32002 unsupported_capability`

### Requirement: User service and on-demand start
The daemon SHALL ship with a systemd user unit (`omaslack.service`). If a client cannot find the socket, it SHALL try `systemctl --user start omaslack` before failing.

#### Scenario: Opening the TUI with the daemon stopped
- **WHEN** the user runs `omaslack tui` and the daemon is not running
- **THEN** the TUI starts the service, waits up to 5 seconds for the socket and connects

### Requirement: Control CLI for scripts
`omaslack ctl <method> [params-json]` SHALL make one RPC call and print the response as JSON. `omaslack ctl --stream <topics>` SHALL print events as NDJSON on stdout and accept NDJSON requests on stdin, so it can bridge clients that cannot open the socket directly.

#### Scenario: Bar script
- **WHEN** `omaslack ctl unread.summary` runs
- **THEN** stdout holds a single JSON object with `dms`, `mentions`, `channels` and `threads` per workspace and in total
