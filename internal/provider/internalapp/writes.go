package internalapp

import "gitlab.com/scabb-island/omaslack/internal/provider"

// The writes beyond posting come from the embedded Web API client
// (slackwire), signed with the user token: chat.update and chat.delete,
// reactions.add and reactions.remove, search.messages, users.list (read
// into a cached directory for user search and for names, since the public
// API has no filtered user search) with users.info for the people it
// lacks, emoji.list, users.setPresence, dnd.setSnooze and dnd.endSnooze,
// and files: files.getUploadURLExternal and files.completeUploadExternal,
// and downloads of url_private_download with the same bearer token. Every
// one accepts a user token with the scopes in docs/slack-app-manifest.yml.
// Typing indicators are not among them: the Events API has no way to send
// one. These assertions keep them from silently disappearing, since
// callers find them with type assertions.
var (
	_ provider.MessageEditor   = (*Provider)(nil)
	_ provider.Reactor         = (*Provider)(nil)
	_ provider.MessageSearcher = (*Provider)(nil)
	_ provider.UserSearcher    = (*Provider)(nil)
	_ provider.UserDirectory   = (*Provider)(nil)
	_ provider.EmojiLister     = (*Provider)(nil)
	_ provider.PresenceSetter  = (*Provider)(nil)
	_ provider.DNDController   = (*Provider)(nil)
	_ provider.FileUploader    = (*Provider)(nil)
	_ provider.FileDownloader  = (*Provider)(nil)
)
