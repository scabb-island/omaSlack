package slackwire

import (
	"cmp"
	"fmt"
	"slices"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// Message is a Slack message as the Web API, the Events API and the web
// client's socket send it.
type Message struct {
	User  string `json:"user"`
	BotID string `json:"bot_id"`
	// A bot's names: the one a legacy integration gives each message,
	// and the bot's own (see authorName).
	Username   string `json:"username"`
	BotProfile struct {
		Name string `json:"name"`
	} `json:"bot_profile"`
	Text     string `json:"text"`
	TS       string `json:"ts"`
	ThreadTS string `json:"thread_ts"`
	Edited   struct {
		TS string `json:"ts"`
	} `json:"edited"`
	Files       []File `json:"files"`
	ReplyCount  int    `json:"reply_count"`  // thread roots only
	LatestReply string `json:"latest_reply"` // thread roots only
}

// File is a file object as messages carry it in their files[] and the
// files.* methods return it.
type File struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Title       string `json:"title"`
	MIMEType    string `json:"mimetype"`
	Size        int64  `json:"size"`
	DownloadURL string `json:"url_private_download"`
	Mode        string `json:"mode"`
}

// unshownModes are the file modes with nothing to show or fetch: a
// deleted file's tombstone, and a file a free plan's history limit hides.
var unshownModes = []string{"tombstone", "hidden_by_limit"}

// Parse checks f and builds its domain.File. A file without a valid ID,
// or in an unshown mode, is reported as not ok.
func (f File) Parse() (domain.File, bool) {
	id, err := domain.ParseFileID(f.ID)
	if err != nil || slices.Contains(unshownModes, f.Mode) {
		return domain.File{}, false
	}
	return domain.File{ID: id, Name: f.Name, Title: f.Title, MIMEType: f.MIMEType, Size: f.Size, DownloadURL: f.DownloadURL}, true
}

// parseFiles keeps the files of ws that can be shown, in order. A file
// that cannot is dropped, never the message it came with.
func parseFiles(ws []File) []domain.File {
	var out []domain.File
	for _, w := range ws {
		if f, ok := w.Parse(); ok {
			out = append(out, f)
		}
	}
	return out
}

// Parse checks w and builds its domain.Message. The author is the user
// when there is one (apps posting as a person send both) and the bot
// otherwise; a message with neither is rejected.
func (w Message) Parse() (domain.Message, error) {
	ts, err := domain.ParseTS(w.TS)
	if err != nil {
		return domain.Message{}, fmt.Errorf("message ts: %w", err)
	}
	author, err := w.author()
	if err != nil {
		return domain.Message{}, fmt.Errorf("message %s author: %w", w.TS, err)
	}
	thread, err := ParseOptionalTS(w.ThreadTS)
	if err != nil {
		return domain.Message{}, fmt.Errorf("message %s thread_ts: %w", w.TS, err)
	}
	edited, err := ParseOptionalTS(w.Edited.TS)
	if err != nil {
		return domain.Message{}, fmt.Errorf("message %s edited.ts: %w", w.TS, err)
	}
	latest, err := ParseOptionalTS(w.LatestReply)
	if err != nil {
		return domain.Message{}, fmt.Errorf("message %s latest_reply: %w", w.TS, err)
	}
	return domain.Message{
		TS: ts, ThreadTS: thread, Author: author, AuthorName: w.authorName(), Text: w.Text, Edited: edited, Files: parseFiles(w.Files),
		ReplyCount: max(w.ReplyCount, 0), LatestReply: latest,
	}, nil
}

func (w Message) author() (domain.Author, error) {
	if w.User != "" {
		user, err := domain.ParseUserID(w.User)
		return domain.UserAuthor(user), err
	}
	bot, err := domain.ParseBotID(w.BotID)
	return domain.BotAuthor(bot), err
}

// authorName is the name Slack shows on a message a bot posted: the
// per-message username a legacy integration sets, else the bot's profile
// name. A message without a bot ID is a person's and has none.
func (w Message) authorName() string {
	if w.BotID == "" {
		return ""
	}
	return cmp.Or(w.Username, w.BotProfile.Name)
}

// ParseOptionalTS parses s, where an empty s means "no timestamp".
func ParseOptionalTS(s string) (domain.TS, error) {
	if s == "" {
		return domain.TS{}, nil
	}
	return domain.ParseTS(s)
}
