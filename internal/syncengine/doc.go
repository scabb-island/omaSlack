// Package syncengine keeps one workspace's Store in step with Slack (spec
// sync-engine). An Engine consumes its provider's real-time stream, writes
// every event to the Store and then publishes it to clients; it reconnects
// with backoff, stops for good when Slack rejects the credentials, waits
// out 429s through the rate budget, fetches history on demand and catches
// up the conversations that matter after every reconnect.
//
// The Engine is the Store's only writer, and that is structural: every
// write runs on the goroutine that called Run.
package syncengine
