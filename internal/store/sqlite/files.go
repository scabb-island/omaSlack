package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/store"
)

// putFiles makes the files of the message at ts exactly files, and
// records each file's metadata. The caller decides whether this version of
// the message may replace the stored one, and runs it in the same
// transaction as the message.
func putFiles(ctx context.Context, ex execer, conv domain.ConversationID, ts domain.TS, files []domain.File) error {
	if _, err := ex.ExecContext(ctx,
		`DELETE FROM message_files WHERE conv_id = ? AND ts = ?`, conv.String(), ts.String()); err != nil {
		return err
	}
	for i, f := range files {
		if _, err := ex.ExecContext(ctx, `
			INSERT INTO files (file_id, name, title, mimetype, size, download_url) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT (file_id) DO UPDATE SET name = excluded.name, title = excluded.title,
			    mimetype = excluded.mimetype, size = excluded.size, download_url = excluded.download_url`,
			f.ID.String(), f.Name, f.Title, f.MIMEType, f.Size, f.DownloadURL); err != nil {
			return err
		}
		if _, err := ex.ExecContext(ctx,
			`INSERT INTO message_files (conv_id, ts, position, file_id) VALUES (?, ?, ?, ?)`,
			conv.String(), ts.String(), i, f.ID.String()); err != nil {
			return err
		}
	}
	return nil
}

// fileColumns are the columns a fileRow scans, in order.
const fileColumns = `files.file_id, files.name, files.title, files.mimetype, files.size, files.download_url`

// fileRow is a files row as stored: outside data until parsed.
type fileRow struct {
	id, name, title, mimetype, url string
	size                           int64
}

func (r *fileRow) targets() []any {
	return []any{&r.id, &r.name, &r.title, &r.mimetype, &r.size, &r.url}
}

func (r fileRow) parse() (domain.File, error) {
	id, err := domain.ParseFileID(r.id)
	if err != nil {
		return domain.File{}, fmt.Errorf("store/sqlite: corrupt file %q: %w", r.id, err)
	}
	return domain.File{ID: id, Name: r.name, Title: r.title, MIMEType: r.mimetype, Size: r.size, DownloadURL: r.url}, nil
}

// File returns a stored file by its ID.
func (s *Store) File(ctx context.Context, id domain.FileID) (domain.File, error) {
	var r fileRow
	err := s.db.QueryRowContext(ctx,
		`SELECT `+fileColumns+` FROM files WHERE file_id = ?`, id.String()).Scan(r.targets()...)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.File{}, fmt.Errorf("%w: %s", store.ErrUnknownFile, id)
	}
	if err != nil {
		return domain.File{}, fmt.Errorf("store/sqlite: read file: %w", err)
	}
	return r.parse()
}

// filesOf returns the files of the message at ts, in order; nil for none.
func (s *Store) filesOf(ctx context.Context, conv domain.ConversationID, ts domain.TS) ([]domain.File, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+fileColumns+` FROM message_files JOIN files USING (file_id)
		WHERE message_files.conv_id = ? AND message_files.ts = ?
		ORDER BY message_files.position`,
		conv.String(), ts.String())
	if err != nil {
		return nil, fmt.Errorf("store/sqlite: read files: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.File
	for rows.Next() {
		var r fileRow
		if err := rows.Scan(r.targets()...); err != nil {
			return nil, fmt.Errorf("store/sqlite: read files: %w", err)
		}
		f, err := r.parse()
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// withFiles fills in the files of each message of msgs, of conv.
func (s *Store) withFiles(ctx context.Context, conv domain.ConversationID, msgs []domain.Message) ([]domain.Message, error) {
	for i := range msgs {
		files, err := s.filesOf(ctx, conv, msgs[i].TS)
		if err != nil {
			return nil, err
		}
		msgs[i].Files = files
	}
	return msgs, nil
}
