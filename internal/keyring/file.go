package keyring

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// DefaultFilePath is where the file backend keeps credentials:
// $XDG_CONFIG_HOME/omaslack/credentials, or ~/.config/omaslack/credentials.
func DefaultFilePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "omaslack", "credentials"), nil
}

// FileStore keeps every workspace's credentials in one JSON file readable
// only by its owner (0600, in a 0700 directory). It is the fallback for
// machines without a Secret Service. Writes replace the file atomically, so
// a crash leaves either the old file or the new one.
type FileStore struct {
	path string
	mu   sync.Mutex
}

// fileDocument is the file's JSON shape.
type fileDocument struct {
	Teams map[string]record `json:"teams"`
}

func NewFileStore(path string) *FileStore {
	return &FileStore{path: path}
}

func (s *FileStore) Backend() Backend { return File }

// Close releases nothing: the file is opened only while it is read or
// written.
func (s *FileStore) Close() error { return nil }

func (s *FileStore) Get(_ context.Context, team domain.TeamID) (Credentials, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.load()
	if err != nil {
		return nil, err
	}
	creds, ok := all[team]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, team)
	}
	return creds, nil
}

func (s *FileStore) Set(_ context.Context, team domain.TeamID, creds Credentials) error {
	return s.update(func(all map[domain.TeamID]Credentials) { all[team] = creds })
}

func (s *FileStore) Delete(_ context.Context, team domain.TeamID) error {
	return s.update(func(all map[domain.TeamID]Credentials) { delete(all, team) })
}

func (s *FileStore) Teams(_ context.Context) ([]domain.TeamID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.load()
	if err != nil {
		return nil, err
	}
	teams := make([]domain.TeamID, 0, len(all))
	for team := range all {
		teams = append(teams, team)
	}
	slices.SortFunc(teams, func(a, b domain.TeamID) int { return cmp.Compare(a.String(), b.String()) })
	return teams, nil
}

// update applies change to the stored credentials under the lock and
// writes the result back.
func (s *FileStore) update(change func(map[domain.TeamID]Credentials)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.load()
	if err != nil {
		return err
	}
	change(all)
	return s.save(all)
}

// load reads and parses the whole file; a missing file holds nothing.
func (s *FileStore) load() (map[domain.TeamID]Credentials, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[domain.TeamID]Credentials{}, nil
	}
	if err != nil {
		return nil, err
	}
	var doc fileDocument
	if json.Unmarshal(data, &doc) != nil {
		return nil, fmt.Errorf("%w: %s is not a JSON document", ErrMalformed, s.path)
	}
	return parseTeams(doc.Teams)
}

func parseTeams(records map[string]record) (map[domain.TeamID]Credentials, error) {
	all := make(map[domain.TeamID]Credentials, len(records))
	for key, r := range records {
		team, err := domain.ParseTeamID(key)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrMalformed, err)
		}
		creds, err := r.credentials()
		if err != nil {
			return nil, err
		}
		all[team] = creds
	}
	return all, nil
}

func (s *FileStore) save(all map[domain.TeamID]Credentials) error {
	doc := fileDocument{Teams: make(map[string]record, len(all))}
	for team, creds := range all {
		r, err := toRecord(creds)
		if err != nil {
			return err
		}
		doc.Teams[team.String()] = r
	}
	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	return writeFileAtomic(s.path, data)
}

// writeFileAtomic writes data to a 0600 temporary file next to path and
// renames it over path. The directory is created, or tightened, to 0700.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	//nolint:gosec // G302 is about files; a directory needs the x bit to be entered
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".credentials-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // a no-op once the rename succeeded
	_, writeErr := tmp.Write(data)
	syncErr := tmp.Sync()
	if err := errors.Join(writeErr, syncErr, tmp.Close()); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
