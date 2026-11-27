package slackwire

import (
	"context"
	"net/url"
	"sync"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// Names for user IDs (provider.UserDirectory).
//
// Users answers from the same users.list directory SearchUsers searches,
// so a workspace pays for one load per directoryTTL whatever clients ask.
// Someone users.list does not have (a guest from a shared channel, a
// user who joined after the load) is looked up with users.info, at most
// maxLookups per call, and remembered until the directory reloads, found
// or not.

// maxLookups bounds the users.info calls one Users call makes. users.info
// is Tier 4, but a client rendering a long history of strangers must not
// turn one request into hundreds of calls; it asks again for the rest.
const maxLookups = 20

// userIndex is the directory as one users.list load left it, plus the
// people looked up since. A reload replaces it whole, which forgets the
// lookups too.
type userIndex struct {
	active []domain.User // not deleted, in users.list order: what SearchUsers searches

	mu      sync.Mutex
	byID    map[domain.UserID]domain.User // every listed user, deleted ones too, and every one found since
	unknown map[domain.UserID]bool        // looked up and not found
}

func newUserIndex() *userIndex {
	return &userIndex{byID: map[domain.UserID]domain.User{}, unknown: map[domain.UserID]bool{}}
}

// list adds a user from users.list. Deleted users are named (old
// messages carry them) but not searched.
func (x *userIndex) list(u domain.User, deleted bool) {
	x.byID[u.ID] = u
	if !deleted {
		x.active = append(x.active, u)
	}
}

// missing returns up to most of ids, without repeats, that are neither
// known nor already looked up in vain.
func (x *userIndex) missing(ids []domain.UserID, most int) []domain.UserID {
	x.mu.Lock()
	defer x.mu.Unlock()
	var out []domain.UserID
	seen := map[domain.UserID]bool{}
	for _, id := range ids {
		_, known := x.byID[id]
		if known || x.unknown[id] || seen[id] || len(out) == most {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// found remembers what a lookup of id answered: u, or nobody when u is
// nil.
func (x *userIndex) found(id domain.UserID, u *domain.User) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if u == nil {
		x.unknown[id] = true
		return
	}
	x.byID[id] = *u
}

// find returns the known users among ids, in their order, once each.
func (x *userIndex) find(ids []domain.UserID) []domain.User {
	x.mu.Lock()
	defer x.mu.Unlock()
	out := []domain.User{}
	seen := map[domain.UserID]bool{}
	for _, id := range ids {
		if u, ok := x.byID[id]; ok && !seen[id] {
			seen[id] = true
			out = append(out, u)
		}
	}
	return out
}

// Users returns the people among ids, in the order asked and once each;
// unknown IDs are left out. See the top of this file for where the
// names come from.
func (a *WebAPI) Users(ctx context.Context, ids []domain.UserID) ([]domain.User, error) {
	idx, err := a.directory(ctx)
	if err != nil {
		return nil, err
	}
	for _, id := range idx.missing(ids, maxLookups) {
		u, err := a.userInfo(ctx, id)
		if err != nil {
			return nil, err
		}
		idx.found(id, u)
	}
	return idx.find(ids), nil
}

type userInfoResponse struct {
	Envelope
	User User `json:"user"`
}

// notFoundCodes are the users.info errors that mean there is no such
// user for this token: nobody to name, not a failure.
var notFoundCodes = []string{"user_not_found", "user_not_visible"}

// userInfo looks id up with users.info: nil when Slack knows no such
// user, or sends one that does not parse (logged).
func (a *WebAPI) userInfo(ctx context.Context, id domain.UserID) (*domain.User, error) {
	var resp userInfoResponse
	err := a.Call(ctx, "users.info", url.Values{"user": {id.String()}}, &resp)
	for _, code := range notFoundCodes {
		if HasCode(err, code) {
			return nil, nil
		}
	}
	if err != nil {
		return nil, err
	}
	u, err := resp.User.Parse()
	if err != nil || u.ID != id {
		a.log.Warn("users.info: dropped a user", "asked", id, "got", resp.User.ID, "err", err)
		return nil, nil
	}
	return &u, nil
}
