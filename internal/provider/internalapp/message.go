package internalapp

import (
	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider/slackwire"
)

// wireMessage is a Slack message as the Web API and the Events API send
// it. The parsing lives in slackwire, shared with the desktop-session
// provider; this name keeps the package's own tests reading as before.
type wireMessage slackwire.Message

// parse checks w and builds its domain.Message (see slackwire.Message.Parse).
func (w wireMessage) parse() (domain.Message, error) { return slackwire.Message(w).Parse() }
