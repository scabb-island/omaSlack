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
	"github.com/syndtr/goleveldb/leveldb/opt"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// localConfigKey is the Local Storage key under which Slack's client keeps
// each workspace's xoxc token. Modern Slack no longer puts the token in
// the page HTML, so this store is the source of truth.
const localConfigKey = "localConfig_v2"

type localConfig struct {
	Teams map[string]struct {
		Token string `json:"token"`
	} `json:"teams"`
}

// readTokens returns each team's xoxc token from the desktop app's Local
// Storage. The LevelDB is read from a copy (see withCopies); its directory
// is flat, so copying its regular files is enough.
func readTokens(dir string) (map[domain.TeamID]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotSignedIn
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
	var tokens map[domain.TeamID]string
	err = withCopies(dir, names, func(copyDir string) (err error) {
		tokens, err = tokensFromLevelDB(copyDir)
		return err
	})
	return tokens, err
}

// tokensFromLevelDB merges the tokens of every localConfig_v2 entry
// (Chromium keeps one per origin). Keys are "_<origin>\x00<tag><key>"; the
// origin is unknown (app.slack.com or a workspace host), so only the
// suffix is matched. Entries that do not parse are skipped.
func tokensFromLevelDB(dir string) (map[domain.TeamID]string, error) {
	db, err := leveldb.OpenFile(dir, &opt.Options{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer db.Close()
	tokens := map[domain.TeamID]string{}
	iter := db.NewIterator(nil, nil)
	defer iter.Release()
	for iter.Next() {
		if strings.HasSuffix(string(iter.Key()), localConfigKey) {
			addTokens(tokens, decodeLocalStorageValue(iter.Value()))
		}
	}
	return tokens, iter.Error()
}

func addTokens(tokens map[domain.TeamID]string, value string) {
	var cfg localConfig
	if json.Unmarshal([]byte(value), &cfg) != nil {
		return
	}
	for id, t := range cfg.Teams {
		if team, err := domain.ParseTeamID(id); err == nil && t.Token != "" {
			tokens[team] = t.Token
		}
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
