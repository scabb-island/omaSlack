package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/store"
)

func file(t *testing.T, id, name string) domain.File {
	t.Helper()
	fid, err := domain.ParseFileID(id)
	if err != nil {
		t.Fatal(err)
	}
	return domain.File{ID: fid, Name: name, Title: "title of " + name, MIMEType: "image/png", Size: 1234,
		DownloadURL: "https://files.slack.com/files-pri/T01-" + id + "/download/" + name}
}

func withFiles(m domain.Message, files ...domain.File) domain.Message {
	m.Files = files
	return m
}

func TestMessagesAndRepliesCarryTheirFilesInOrder(t *testing.T) {
	s := openStore(t)
	root := withFiles(msg(t, "1700000001.000000", "root"), file(t, "F02", "b.png"), file(t, "F01", "a.png"))
	root.ThreadTS = root.TS
	reply := withFiles(msg(t, "1700000002.000000", "reply"), file(t, "F03", "c.png"))
	reply.ThreadTS = root.TS
	mustPutHistory(t, s, "C01", root)
	mustApply(t, s, created(t, "C01", reply))

	if got := messages(t, s, "C01", zeroTS, 10); !reflect.DeepEqual(got, []domain.Message{root}) {
		t.Errorf("Messages = %+v\nwant [%+v]", got, root)
	}
	if got := replies(t, s, "C01", "1700000001.000000"); !reflect.DeepEqual(got, []domain.Message{reply}) {
		t.Errorf("Replies = %+v\nwant [%+v]", got, reply)
	}
}

func TestStoringFilesTwiceIsIdempotent(t *testing.T) {
	s := openStore(t)
	m := withFiles(msg(t, "1700000001.000000", "pics"), file(t, "F01", "a.png"))
	mustPutHistory(t, s, "C01", m)
	mustPutHistory(t, s, "C01", m)
	mustApply(t, s, created(t, "C01", m))
	if got := messages(t, s, "C01", zeroTS, 10); !reflect.DeepEqual(got, []domain.Message{m}) {
		t.Errorf("Messages = %+v\nwant [%+v]", got, m)
	}
}

// An edit's result (chat.update) may leave files out: the stored ones stay.
// An edit that carries files replaces them.
func TestAnEditKeepsOrReplacesTheFiles(t *testing.T) {
	s := openStore(t)
	m := withFiles(msg(t, "1700000001.000000", "pics"), file(t, "F01", "a.png"), file(t, "F02", "b.png"))
	mustApply(t, s, created(t, "C01", m))

	mustApply(t, s, updated(t, "C01", editedMsg(t, "1700000001.000000", "fixed", "1700000002.000000")))
	if got := messages(t, s, "C01", zeroTS, 10); len(got) != 1 || !reflect.DeepEqual(got[0].Files, m.Files) {
		t.Fatalf("files after an edit without files = %+v, want %+v", got, m.Files)
	}

	edit := withFiles(editedMsg(t, "1700000001.000000", "one left", "1700000003.000000"), file(t, "F02", "b.png"))
	mustApply(t, s, updated(t, "C01", edit))
	if got := messages(t, s, "C01", zeroTS, 10); !reflect.DeepEqual(got, []domain.Message{edit}) {
		t.Fatalf("Messages = %+v\nwant [%+v]", got, edit)
	}

	// An older version never brings the removed file back.
	mustApply(t, s, created(t, "C01", m))
	if got := messages(t, s, "C01", zeroTS, 10); !reflect.DeepEqual(got, []domain.Message{edit}) {
		t.Fatalf("Messages after a stale create = %+v\nwant [%+v]", got, edit)
	}
}

func TestFileLooksAStoredFileUp(t *testing.T) {
	s := openStore(t)
	want := file(t, "F01", "a.png")
	mustPutHistory(t, s, "C01", withFiles(msg(t, "1700000001.000000", "pic"), want))

	got, err := s.File(context.Background(), want.ID)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("File = %+v, %v; want %+v", got, err, want)
	}
	unknown, _ := domain.ParseFileID("F99")
	if _, err := s.File(context.Background(), unknown); !errors.Is(err, store.ErrUnknownFile) {
		t.Fatalf("File(unknown) err = %v, want ErrUnknownFile", err)
	}
}

func TestACorruptFileRowIsNeverTrusted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "T123.db")
	s := openAt(t, path)
	rawExec(t, path, `INSERT INTO messages (conv_id, ts, author) VALUES ('C01', '1700000001.000000', 'U01')`)
	rawExec(t, path, `INSERT INTO files (file_id, name) VALUES ('../etc', 'x')`)
	rawExec(t, path, `INSERT INTO message_files (conv_id, ts, position, file_id) VALUES ('C01', '1700000001.000000', 0, '../etc')`)
	if got, err := s.Messages(context.Background(), conv(t, "C01"), zeroTS, 10); err == nil {
		t.Fatalf("Messages served a corrupt file: %+v", got)
	}
}
