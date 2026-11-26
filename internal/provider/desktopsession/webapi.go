package desktopsession

import "gitlab.com/scabb-island/omaslack/internal/provider"

// The writes beyond posting come from the embedded Web API client
// (slackwire), signed like every other request with the xoxc token and
// the d cookie: chat.update and chat.delete, reactions.add and
// reactions.remove, search.messages, users.list (read into a cached
// directory for user search), users.setPresence, and dnd.setSnooze and
// dnd.endSnooze. They are the documented methods, which accept the web
// client's credentials; the web client's own people search (an
// undocumented edge API) is not used. These assertions keep them from
// silently disappearing, since callers find them with type assertions.
var (
	_ provider.MessageEditor   = (*Provider)(nil)
	_ provider.Reactor         = (*Provider)(nil)
	_ provider.MessageSearcher = (*Provider)(nil)
	_ provider.UserSearcher    = (*Provider)(nil)
	_ provider.PresenceSetter  = (*Provider)(nil)
	_ provider.DNDController   = (*Provider)(nil)
)
