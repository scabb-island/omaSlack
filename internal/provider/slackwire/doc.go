// Package slackwire is what both Slack providers share about Slack's wire
// format: the message and event JSON that the Web API, the Events API and
// the web client's socket all use, its mapping to domain types, and the
// Web API calls behind provider.Provider's History, Replies, Post and
// MarkRead. The providers differ only in how they authenticate and where
// their events come from.
package slackwire
