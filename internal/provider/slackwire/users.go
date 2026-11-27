package slackwire

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// User search (provider.UserSearcher).
//
// Slack has no documented user search: the web client's people search
// is an undocumented edge API on another host, and the only documented
// ways to find users are users.list (the whole workspace) and lookups by
// exact email. So both providers read users.list once into a directory,
// keep it for directoryTTL, and search it locally. A workspace of 5000
// people is 25 pages (users.list is Tier 2, about 20 a minute) once per
// TTL; every keystroke after that is free.

// usersPageSize is how many users users.list returns per page; 200 is
// the most Slack recommends.
const usersPageSize = 200

// directoryTTL is how long a loaded directory answers searches.
// People join and change their names rarely; a newcomer shows up within
// this long.
const directoryTTL = 30 * time.Minute

// User is a member as users.list sends it.
type User struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Deleted bool   `json:"deleted"`
	Profile struct {
		RealName    string `json:"real_name"`
		DisplayName string `json:"display_name"`
	} `json:"profile"`
}

// Parse checks w and builds its domain.User.
func (w User) Parse() (domain.User, error) {
	id, err := domain.ParseUserID(w.ID)
	if err != nil {
		return domain.User{}, fmt.Errorf("user id: %w", err)
	}
	return domain.User{ID: id, Name: w.Name, RealName: w.Profile.RealName, DisplayName: w.Profile.DisplayName}, nil
}

type usersResponse struct {
	Envelope
	Members  []User `json:"members"`
	Metadata struct {
		NextCursor string `json:"next_cursor"`
	} `json:"response_metadata"`
}

// SearchUsers returns up to limit users (every match when limit < 1)
// whose handle, real name or display name contains query, ignoring case
// and a leading "@". Users with a field that starts with query come
// first; the rest keep the directory's order. Deleted users are left
// out; bots are not, since people message them too. A blank query
// matches nobody.
func (a *WebAPI) SearchUsers(ctx context.Context, query string, limit int) ([]domain.User, error) {
	idx, err := a.directory(ctx)
	if err != nil {
		return nil, err
	}
	return matchUsers(idx.active, query, limit), nil
}

// directory returns the directory, loading it first when it is missing
// or stale.
func (a *WebAPI) directory(ctx context.Context) (*userIndex, error) {
	return a.dir.get(a.now(), directoryTTL, func() (*userIndex, error) { return a.listUsers(ctx) })
}

// listUsers calls users.list until the last page and indexes every user
// in it. Users that do not parse are logged and dropped.
func (a *WebAPI) listUsers(ctx context.Context) (*userIndex, error) {
	form := url.Values{"limit": {strconv.Itoa(usersPageSize)}}
	idx := newUserIndex()
	for {
		var resp usersResponse
		if err := a.Call(ctx, "users.list", form, &resp); err != nil {
			return nil, err
		}
		for _, w := range resp.Members {
			u, err := w.Parse()
			if err != nil {
				a.log.Warn("dropped a user", "err", err)
				continue
			}
			idx.list(u, w.Deleted)
		}
		if resp.Metadata.NextCursor == "" {
			return idx, nil
		}
		form.Set("cursor", resp.Metadata.NextCursor)
	}
}

// matchUsers is the search itself: see SearchUsers.
func matchUsers(users []domain.User, query string, limit int) []domain.User {
	q := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(query), "@"))
	var prefixed, contained []domain.User
	for _, u := range users {
		switch matchRank(u, q) {
		case startsWith:
			prefixed = append(prefixed, u)
		case contains:
			contained = append(contained, u)
		}
	}
	found := append(append([]domain.User{}, prefixed...), contained...)
	if limit > 0 && len(found) > limit {
		found = found[:limit]
	}
	return found
}

type rank int

const (
	noMatch rank = iota
	contains
	startsWith
)

// matchRank is how well u matches q, a lower-case query: the best of its
// fields. A blank q matches nobody.
func matchRank(u domain.User, q string) rank {
	best := noMatch
	if q == "" {
		return best
	}
	for _, field := range []string{u.Name, u.RealName, u.DisplayName} {
		field = strings.ToLower(field)
		switch {
		case strings.HasPrefix(field, q):
			return startsWith
		case strings.Contains(field, q):
			best = contains
		}
	}
	return best
}
