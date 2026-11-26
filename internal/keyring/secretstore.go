package keyring

import (
	"context"
	"fmt"
	"slices"

	"github.com/godbus/dbus/v5"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/keyring/secretservice"
)

const (
	serviceAttr = "service"
	serviceName = "omaslack"
	teamAttr    = "team"
)

// secrets is the part of a Secret Service session the store uses.
// *secretservice.Session implements it; tests use an in-memory fake.
type secrets interface {
	Search(ctx context.Context, attrs map[string]string) (unlocked, locked []dbus.ObjectPath, err error)
	Secret(ctx context.Context, item dbus.ObjectPath) ([]byte, error)
	Attributes(ctx context.Context, item dbus.ObjectPath) (map[string]string, error)
	Create(ctx context.Context, label string, attrs map[string]string, value []byte, contentType string) error
	Delete(ctx context.Context, item dbus.ObjectPath) error
	Close(ctx context.Context) error
}

// SecretServiceStore keeps one Secret Service item per workspace, with the
// attributes service=omaslack and team=<team ID>; the secret is the JSON
// record of the credentials. Each operation opens its own session, so a
// keyring daemon that restarts under a long-running daemon is no problem.
type SecretServiceStore struct {
	open      func(context.Context) (secrets, error)
	closeConn func() error
}

func newSecretServiceStore(open func(context.Context) (secrets, error), closeConn func() error) *SecretServiceStore {
	return &SecretServiceStore{open: open, closeConn: closeConn}
}

func (s *SecretServiceStore) Backend() Backend { return SecretService }

// Close closes the store's D-Bus connection.
func (s *SecretServiceStore) Close() error { return s.closeConn() }

func (s *SecretServiceStore) Get(ctx context.Context, team domain.TeamID) (Credentials, error) {
	var creds Credentials
	err := s.withSession(ctx, func(ss secrets) error {
		unlocked, locked, err := ss.Search(ctx, teamAttrs(team))
		if err != nil {
			return err
		}
		if len(unlocked) == 0 {
			return missing(team, locked)
		}
		data, err := ss.Secret(ctx, unlocked[0])
		if err != nil {
			return err
		}
		creds, err = decode(data)
		return err
	})
	return creds, err
}

// missing explains a search with no unlocked match: either the item is
// there behind a lock, or there is none.
func missing(team domain.TeamID, locked []dbus.ObjectPath) error {
	if len(locked) > 0 {
		return secretservice.ErrLocked
	}
	return fmt.Errorf("%w: %s", ErrNotFound, team)
}

func (s *SecretServiceStore) Set(ctx context.Context, team domain.TeamID, creds Credentials) error {
	data, err := encode(creds)
	if err != nil {
		return err
	}
	return s.withSession(ctx, func(ss secrets) error {
		return ss.Create(ctx, "omaslack credentials for "+team.String(), teamAttrs(team), data, "application/json")
	})
}

func (s *SecretServiceStore) Delete(ctx context.Context, team domain.TeamID) error {
	return s.withSession(ctx, func(ss secrets) error {
		items, err := search(ctx, ss, teamAttrs(team))
		if err != nil {
			return err
		}
		for _, item := range items {
			if err := ss.Delete(ctx, item); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *SecretServiceStore) Teams(ctx context.Context) ([]domain.TeamID, error) {
	var teams []domain.TeamID
	err := s.withSession(ctx, func(ss secrets) error {
		items, err := search(ctx, ss, map[string]string{serviceAttr: serviceName})
		if err != nil {
			return err
		}
		teams, err = teamsOf(ctx, ss, items)
		return err
	})
	return teams, err
}

// teamsOf reads each item's team attribute, parsed once here, and returns
// the distinct teams sorted. Attributes are readable on locked items too.
func teamsOf(ctx context.Context, ss secrets, items []dbus.ObjectPath) ([]domain.TeamID, error) {
	teams := make([]domain.TeamID, 0, len(items))
	for _, item := range items {
		attrs, err := ss.Attributes(ctx, item)
		if err != nil {
			return nil, err
		}
		team, err := domain.ParseTeamID(attrs[teamAttr])
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrMalformed, err)
		}
		teams = append(teams, team)
	}
	sortTeams(teams)
	return slices.Compact(teams), nil
}

// search returns every match, locked or not.
func search(ctx context.Context, ss secrets, attrs map[string]string) ([]dbus.ObjectPath, error) {
	unlocked, locked, err := ss.Search(ctx, attrs)
	return append(unlocked, locked...), err
}

func teamAttrs(team domain.TeamID) map[string]string {
	return map[string]string{serviceAttr: serviceName, teamAttr: team.String()}
}

func (s *SecretServiceStore) withSession(ctx context.Context, fn func(secrets) error) error {
	ss, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = ss.Close(ctx) }()
	return fn(ss)
}
