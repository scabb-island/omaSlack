## ADDED Requirements

### Requirement: Real-time reception
The sync engine SHALL consume the provider's event stream, normalize it into domain events (`message.created`, `message.updated`, `message.deleted`, `reaction.changed`, `conversation.updated`, `read.changed`, `presence.changed`, `typing`), persist them in the Store and publish them to subscribed clients.

#### Scenario: A new message arrives
- **WHEN** someone posts a message in a channel the user is a member of
- **THEN** the message is saved in the Store and subscribed clients receive `message.created` less than 1 second after the provider event arrived

### Requirement: Reconnection with backoff
On disconnection, the engine SHALL retry with exponential backoff and jitter, between 1 and 30 seconds, and publish `connection.state` (`connecting`, `connected`, `disconnected`) on every transition.

#### Scenario: The network drops
- **WHEN** the network connection is lost for 2 minutes
- **THEN** clients see the `disconnected` state and then `connecting`, and when the network returns the state moves to `connected` with no user action

### Requirement: Reconciliation after reconnecting
After reconnecting, the engine SHALL fetch what happened during the outage in active conversations (at least those open in any client and those with mentions or DMs) and update the unread counters.

#### Scenario: Messages during the outage
- **WHEN** 3 DMs arrived while the daemon was disconnected
- **THEN** after reconnecting the 3 messages are in the Store and the DM counter shows 3 unread

#### Scenario: Provider list without read state
- **WHEN** the provider's conversation list has no latest message per conversation (the internal-app provider)
- **THEN** the engine fills it in the background, direct messages first, then channels, then only the group DMs known to be active, and unread counts converge without blocking the clients

### Requirement: Local SQLite cache
The Store SHALL be one SQLite database per workspace, in WAL mode, with versioned migrations. It SHALL be treated as a cache: deleting it MUST NOT lose user data, except unsent drafts and local preferences.

#### Scenario: Cold start with a cache
- **WHEN** the daemon starts and a cache from the previous session exists
- **THEN** clients can show the sidebar and the latest messages from the cache before reconnection finishes

#### Scenario: Cache deleted
- **WHEN** the user deletes the cache directory and restarts the daemon
- **THEN** the daemon rebuilds the cache from Slack without asking for credentials again

### Requirement: On-demand backfill
A conversation's history SHALL be fetched in pages when a client opens it or asks for older messages, instead of downloading the whole workspace at startup.

#### Scenario: Scrolling back
- **WHEN** the user reaches the oldest loaded message of a conversation
- **THEN** the daemon fetches the previous page and the client prepends it without losing the scroll position

### Requirement: Rate-limit budget
The engine SHALL keep a budget per API method and tier, honor `Retry-After` and give user requests (opening a conversation, sending) priority over background work (backfill, presence). When the provider is an app that is neither in the Marketplace nor internal, it SHALL apply the 1-call-per-minute limit to `conversations.history` and `conversations.replies`.

#### Scenario: User request while backfill used up the budget
- **WHEN** a backfill is running and the user opens a conversation
- **THEN** the user's request jumps the queue and the backfill pauses until budget is available again

#### Scenario: Slack answers 429
- **WHEN** Slack answers HTTP 429 with `Retry-After: 30`
- **THEN** no calls to that method are made for 30 seconds and clients receive a `connection.state` event with the `rate_limited` detail
