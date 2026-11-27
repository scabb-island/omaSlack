package slackwire

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// must runs one of the domain parsers and fails the test on an error.
func must[T any](t *testing.T, parser func(string) (T, error), s string) T {
	t.Helper()
	v, err := parser(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// decode reads a Message from its JSON.
func decode(t *testing.T, js string) Message {
	t.Helper()
	var m Message
	if err := json.Unmarshal([]byte(js), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMessagesParseIntoDomainMessages(t *testing.T) {
	ts := func(s string) domain.TS { return must(t, domain.ParseTS, s) }
	author := func(s string) domain.Author { return must(t, domain.ParseAuthor, s) }
	for name, tc := range map[string]struct {
		json string
		want domain.Message
	}{
		"plain": {`{"user":"U0002","text":"hi","ts":"1700000100.000100"}`,
			domain.Message{TS: ts("1700000100.000100"), Author: author("U0002"), Text: "hi"}},
		"bot": {`{"bot_id":"B0001","text":"deployed","ts":"1700000100.000100"}`,
			domain.Message{TS: ts("1700000100.000100"), Author: author("B0001"), Text: "deployed"}},
		// Apps posting as a person send both; the person is the author.
		"user and bot": {`{"user":"U0002","bot_id":"B0001","ts":"1700000100.000100"}`,
			domain.Message{TS: ts("1700000100.000100"), Author: author("U0002")}},
		"edited reply": {`{"user":"U0002","ts":"1700000300.000300","thread_ts":"1700000100.000100","edited":{"ts":"1700000400.000000"}}`,
			domain.Message{TS: ts("1700000300.000300"), ThreadTS: ts("1700000100.000100"), Author: author("U0002"), Edited: ts("1700000400.000000")}},
		"thread root": {`{"user":"U0002","ts":"1700000100.000100","thread_ts":"1700000100.000100","reply_count":3,"latest_reply":"1700000500.000500"}`,
			domain.Message{TS: ts("1700000100.000100"), ThreadTS: ts("1700000100.000100"), Author: author("U0002"),
				ReplyCount: 3, LatestReply: ts("1700000500.000500")}},
		"with files": {`{"user":"U0002","ts":"1700000100.000100","files":[` +
			`{"id":"F0001","name":"report.pdf","title":"Q3 report","mimetype":"application/pdf","size":2048,` +
			`"url_private":"https://files.slack.com/files-pri/T0001-F0001/report.pdf",` +
			`"url_private_download":"https://files.slack.com/files-pri/T0001-F0001/download/report.pdf","mode":"hosted"},` +
			`{"id":"F0002","name":"doc","mimetype":"application/vnd.google-apps.document","mode":"external"}]}`,
			domain.Message{TS: ts("1700000100.000100"), Author: author("U0002"), Files: []domain.File{
				{ID: must(t, domain.ParseFileID, "F0001"), Name: "report.pdf", Title: "Q3 report", MIMEType: "application/pdf", Size: 2048,
					DownloadURL: "https://files.slack.com/files-pri/T0001-F0001/download/report.pdf"},
				{ID: must(t, domain.ParseFileID, "F0002"), Name: "doc", MIMEType: "application/vnd.google-apps.document"},
			}}},
		// Deleted files stay as tombstones and files past a free plan's
		// limit are hidden: neither can be shown or fetched. A file with a
		// bad ID is dropped rather than the whole message.
		"files that cannot be shown": {`{"user":"U0002","ts":"1700000100.000100","files":[` +
			`{"id":"F0003","mode":"tombstone"},{"id":"F0004","mode":"hidden_by_limit"},{"id":"../x","name":"x"}]}`,
			domain.Message{TS: ts("1700000100.000100"), Author: author("U0002")}},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := decode(t, tc.json).Parse()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Parse() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestMessagesWithABadFieldAreRejected(t *testing.T) {
	for name, tc := range map[string]struct {
		json    string
		wantErr error
	}{
		"missing ts":       {`{"user":"U0001"}`, domain.ErrInvalidTS},
		"no user, no bot":  {`{"ts":"1700000100.000100"}`, domain.ErrInvalidID},
		"bad user":         {`{"user":"C0001","ts":"1700000100.000100"}`, domain.ErrInvalidID},
		"bad bot_id":       {`{"bot_id":"U0001","ts":"1700000100.000100"}`, domain.ErrInvalidID},
		"bad thread_ts":    {`{"user":"U0001","ts":"1700000100.000100","thread_ts":"1.2"}`, domain.ErrInvalidTS},
		"bad edited.ts":    {`{"user":"U0001","ts":"1700000100.000100","edited":{"ts":"x"}}`, domain.ErrInvalidTS},
		"bad latest_reply": {`{"user":"U0001","ts":"1700000100.000100","latest_reply":"x"}`, domain.ErrInvalidTS},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decode(t, tc.json).Parse(); !errors.Is(err, tc.wantErr) {
				t.Fatalf("Parse() err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
