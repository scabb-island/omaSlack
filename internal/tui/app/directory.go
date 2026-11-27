package app

import (
	"gitlab.com/scabb-island/omaslack/internal/tui/render"
)

// directory is render.Directory for one workspace, answered from State:
// names from users.info and conversations.list, custom emoji from
// emoji.list.
type directory struct {
	st   *State
	team string
}

var _ render.Directory = directory{}

func (d directory) UserName(id string) string { return d.st.UserName(d.team, id) }

func (d directory) ConversationName(id string) string {
	c, ok := d.st.Conversation(d.team, id)
	if !ok {
		return ""
	}
	return d.st.Title(d.team, c)[1:]
}

// Emoji knows the workspace's custom emoji only; standard ones are
// mrkdwn's (task 4.9), so they are reported unknown here.
func (d directory) Emoji(name string) (string, bool) {
	if _, ok := d.st.CustomEmoji(d.team, name); ok {
		return ":" + name + ":", true
	}
	return "", false
}

// Self is the signed-in user, as hello reported it; "" while the daemon
// does not know.
func (d directory) Self() string { return d.st.Workspace(d.team).Self }
