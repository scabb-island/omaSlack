package cli

import (
	"context"
	"errors"
	"fmt"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/keyring"
)

// authUsage is the usage of `omaslack auth`.
const authUsage = `Usage: omaslack auth add --provider internal-app
       omaslack auth add --provider desktop-session [--browser]
       omaslack auth refresh [team-id]
       omaslack auth list
       omaslack auth remove <team-id>
`

// fileBackendWarning is what `omaslack auth` says (doctor has a shorter one)
// when the credentials are in the file backend (spec slack-connection,
// "Credentials in the keyring").
const fileBackendWarning = "Warning: no Secret Service is running, so the credentials are in a plain file " +
	"readable only by you. Start one (for example gnome-keyring) and add the workspaces again to move them there."

// runAuth adds, lists or removes the workspaces whose credentials are in
// the keyring.
func runAuth(env Env, args []string) error {
	if len(args) == 0 {
		return authUsageError("missing subcommand")
	}
	ctx := context.Background()
	switch args[0] {
	case "add":
		return authAdd(ctx, env, args[1:])
	case "refresh":
		return authRefresh(ctx, env, args[1:])
	case "list":
		return authList(ctx, env, args[1:])
	case "remove":
		return authRemove(ctx, env, args[1:])
	}
	return authUsageError("unknown subcommand %q", args[0])
}

func authUsageError(format string, a ...any) error {
	return &usageError{err: fmt.Errorf(format, a...), usage: authUsage}
}

func openKeyring(ctx context.Context, env Env) (keyring.ClosableStore, error) {
	ks, err := env.Keyring(ctx)
	if err != nil {
		return nil, fmt.Errorf("open the keyring: %w", err)
	}
	return ks, nil
}

// printBackend says where the credentials are, with a warning for the
// file backend.
func printBackend(env Env, b keyring.Backend) {
	fmt.Fprintf(env.Stdout, "Credentials are stored in: %s\n", b)
	if b == keyring.File {
		fmt.Fprintln(env.Stdout, fileBackendWarning)
	}
}

func authList(ctx context.Context, env Env, args []string) error {
	if len(args) > 0 {
		return authUsageError("auth list takes no arguments")
	}
	ks, err := openKeyring(ctx, env)
	if err != nil {
		return err
	}
	defer ks.Close()
	teams, err := ks.Teams(ctx)
	if err != nil {
		return err
	}
	if len(teams) == 0 {
		fmt.Fprintln(env.Stdout, "No workspaces. Add one with: omaslack auth add --provider internal-app|desktop-session")
		return nil
	}
	// Team IDs have at most 11 characters.
	fmt.Fprintf(env.Stdout, "%-12s %s\n", "TEAM", "PROVIDER")
	for _, team := range teams {
		creds, err := ks.Get(ctx, team)
		if err != nil {
			return err
		}
		fmt.Fprintf(env.Stdout, "%-12s %s\n", team, creds.Provider())
	}
	printBackend(env, ks.Backend())
	return nil
}

func authRemove(ctx context.Context, env Env, args []string) error {
	if len(args) != 1 {
		return authUsageError("auth remove takes one team ID")
	}
	team, err := domain.ParseTeamID(args[0])
	if err != nil {
		return authUsageError("%v", err)
	}
	ks, err := openKeyring(ctx, env)
	if err != nil {
		return err
	}
	defer ks.Close()
	if _, err := ks.Get(ctx, team); errors.Is(err, keyring.ErrNotFound) {
		fmt.Fprintf(env.Stdout, "No credentials stored for %s; nothing to remove.\n", team)
		return nil
	}
	if err := ks.Delete(ctx, team); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "Removed %s.\n", team)
	return nil
}
