// Adapted from github.com/gammons/slk internal/slackdesktop/localstorage.go (MIT, Copyright (c) 2026 Grant Ammons).

package auth

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"strings"
	"unicode/utf16"

	"github.com/syndtr/goleveldb/leveldb"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// localConfigKey is the Local Storage key under which Slack's client keeps
// each workspace's xoxc token. Modern Slack no longer puts the token in
// the page HTML, so this store is the source of truth.
const localConfigKey = "localConfig_v2"

type localConfig struct {
	Teams map[string]struct {
		Token string `json:"token"`
		Name  string `json:"name"`
		URL   string `json:"url"`
	} `json:"teams"`
}

// localSession is one workspace's web session as the Local Storage holds
// it: the xoxc token, and the name and URL the client keeps alongside it
// (a browser profile has no root-state.json, so these come from here).
type localSession struct {
	Team  domain.TeamID
	Token string
	Name  string
	URL   string
}

// readTokens returns each team's xoxc token from the desktop app's Local
// Storage, keyed by team. It is the desktop path, which matches the tokens
// against the app's own workspace list.
func readTokens(dir string) (map[domain.TeamID]string, error) {
	sessions, err := readSessions(dir)
	if err != nil {
		return nil, err
	}
	tokens := make(map[domain.TeamID]string, len(sessions))
	for _, s := range sessions {
		tokens[s.Team] = s.Token
	}
	return tokens, nil
}

// readSessions returns every workspace web session in the Local Storage.
// The LevelDB is read from a copy (see withCopies); its directory is flat,
// so copying its regular files is enough. No Local Storage means no
// sessions.
func readSessions(dir string) ([]localSession, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.Type().IsRegular() {
			names = append(names, e.Name())
		}
	}
	var sessions []localSession
	err = withCopies(dir, names, func(copyDir string) (err error) {
		sessions, err = sessionsFromLevelDB(copyDir)
		return err
	})
	return sessions, err
}

// sessionsFromLevelDB merges the sessions of every localConfig_v2 entry
// (Chromium keeps one per origin). Keys are "_<origin>\x00<tag><key>"; the
// origin is unknown (app.slack.com or a workspace host), so only the
// suffix is matched. Entries that do not parse are skipped, and a team
// seen twice keeps its last entry.
func sessionsFromLevelDB(dir string) ([]localSession, error) {
	// dir is a throwaway copy, so opening it read-write cannot touch
	// Slack's profile.
	db, err := leveldb.OpenFile(dir, nil)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	byTeam := map[domain.TeamID]localSession{}
	var order []domain.TeamID
	iter := db.NewIterator(nil, nil)
	defer iter.Release()
	for iter.Next() {
		if strings.HasSuffix(string(iter.Key()), localConfigKey) {
			addSessions(byTeam, &order, decodeLocalStorageValue(iter.Value()))
		}
	}
	if err := iter.Error(); err != nil {
		return nil, err
	}
	sessions := make([]localSession, 0, len(order))
	for _, team := range order {
		sessions = append(sessions, byTeam[team])
	}
	return sessions, nil
}

func addSessions(byTeam map[domain.TeamID]localSession, order *[]domain.TeamID, value string) {
	var cfg localConfig
	if json.Unmarshal([]byte(value), &cfg) != nil {
		return
	}
	for id, t := range cfg.Teams {
		team, err := domain.ParseTeamID(id)
		if err != nil || t.Token == "" {
			continue
		}
		if _, seen := byTeam[team]; !seen {
			*order = append(*order, team)
		}
		byTeam[team] = localSession{Team: team, Token: t.Token, Name: t.Name, URL: t.URL}
	}
}

// decodeLocalStorageValue decodes a Chromium Local Storage value, whose
// first byte tags the encoding: 0x00 UTF-16LE, 0x01 Latin-1 (used for
// all-ASCII values such as this JSON). Untagged values are returned as is.
func decodeLocalStorageValue(v []byte) string {
	if len(v) == 0 {
		return ""
	}
	switch v[0] {
	case 0x00:
		return decodeUTF16LE(v[1:])
	case 0x01:
		return string(v[1:])
	default:
		return string(v)
	}
}

func decodeUTF16LE(b []byte) string {
	u16 := make([]uint16, len(b)/2)
	for i := range u16 {
		u16[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	return string(utf16.Decode(u16))
}
