package cli

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"gitlab.com/scabb-island/omaslack/internal/console"
	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/keyring"
	"gitlab.com/scabb-island/omaslack/internal/provider"
	"gitlab.com/scabb-island/omaslack/internal/provider/desktopsession"
	"gitlab.com/scabb-island/omaslack/internal/provider/desktopsession/auth"
	"gitlab.com/scabb-island/omaslack/internal/provider/internalapp"
)

// internalAppGuide is where the internal-app provider's setup is explained.
const internalAppGuide = "https://gitlab.com/scabb-island/omaslack/-/blob/main/docs/internal-app-setup.md"

// termsWarning is the consent the desktop-session provider requires (spec
// slack-connection, "Explicit consent for the unofficial protocol").
const termsWarning = `The desktop-session provider signs in with your Slack desktop app or browser
session and talks to Slack through its unofficial web client protocol. This
may violate Slack's terms of service, and Slack may change the protocol or
end the session at any time.

`

const declinedHelp = `Nothing was stored. The internal-app provider uses only official Slack APIs.
Set it up with ` + internalAppGuide + `
and run: omaslack auth add --provider internal-app
`

const browserSteps = `Sign in with a browser session instead:
  1. Open https://app.slack.com in your browser and sign in.
  2. In DevTools > Network, filter on api/, click a channel, then right click
     one of the requests > Copy > Copy as cURL.
  3. Paste it below and press Enter. Nothing is shown while you paste.
`

// boundSessionHelp explains a session Slack rejects although it works in
// Slack's own app or browser, as seen on a real workspace (feasibility.md,
// field report of 2026-11-26).
// It is all a freshly pasted session gets: pasting another will not help.
const boundSessionHelp = `Some workspaces bind sessions to the Slack app or browser (security
policies, approved clients) and reject them outside its own client, so
omaslack cannot reuse them. Use the official provider instead:
  omaslack auth add --provider internal-app
Set it up as ` + internalAppGuide + ` explains.`

// desktopRejectedHelp is for a session read from the desktop app's files,
// which may only be stale.
const desktopRejectedHelp = `If the desktop app's session is just stale, sign in to the app again,
or paste a browser session:
  omaslack auth add --provider desktop-session --browser
` + boundSessionHelp

var (
	errNotAccepted = errors.New("terms not accepted")
	errNoTerminal  = errors.New("omaslack auth add needs an interactive terminal: " +
		"run it in a terminal, not with its input piped from a file or a script")
)

// newWorkspace is a workspace ready to be stored.
type newWorkspace struct {
	team  domain.TeamID
	name  string
	creds keyring.Credentials
}

// adder runs `omaslack auth add` with the user at con.
type adder struct {
	ctx context.Context
	env Env
	con console.Console
}

func authAdd(ctx context.Context, env Env, args []string) error {
	flow, err := parseAdd(args)
	if err != nil {
		return err
	}
	// Every flow asks for consent or secrets. Without a terminal, an
	// answer would read as the end of the input: a misleading "no".
	if !env.StdinIsTerminal {
		return errNoTerminal
	}
	a := adder{ctx: ctx, env: env, con: console.Console{In: env.Stdin, Out: env.Stdout}}
	added, err := flow(a)
	if err != nil {
		return err
	}
	return a.store(added)
}

// parseAdd returns the flow the arguments of `auth add` ask for.
func parseAdd(args []string) (func(adder) ([]newWorkspace, error), error) {
	fs := flag.NewFlagSet("auth add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	provider := fs.String("provider", "", "")
	browser := fs.Bool("browser", false, "")
	if err := fs.Parse(args); err != nil {
		return nil, authUsageError("%v", err)
	}
	if fs.NArg() > 0 {
		return nil, authUsageError("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	switch {
	case *provider == "internal-app" && !*browser:
		return adder.internalApp, nil
	case *provider == "desktop-session":
		return func(a adder) ([]newWorkspace, error) { return a.desktopSession(*browser) }, nil
	}
	return nil, authUsageError("--provider must be internal-app or desktop-session (--browser goes with desktop-session)")
}

// store saves the added workspaces' credentials, opening the keyring only
// once everything is accepted and checked.
func (a adder) store(added []newWorkspace) error {
	ks, err := openKeyring(a.ctx, a.env)
	if err != nil {
		return err
	}
	defer ks.Close()
	for _, w := range added {
		if err := ks.Set(a.ctx, w.team, w.creds); err != nil {
			return fmt.Errorf("store %s: %w", w.team, err)
		}
		fmt.Fprintf(a.env.Stdout, "Added %s (%s) with %s.\n", w.name, w.team, w.creds.Provider())
	}
	printBackend(a.env, ks.Backend())
	return nil
}

// internalApp asks for the internal app's tokens and checks the user
// token with Slack, which also names the workspace.
func (a adder) internalApp() ([]newWorkspace, error) {
	fmt.Fprintf(a.env.Stdout, "Create the app and its tokens as %s explains.\n", internalAppGuide)
	user, err := a.askToken("User token (xoxp-...): ", "user token", "xoxp-")
	if err != nil {
		return nil, err
	}
	app, err := a.askToken("App-level token (xapp-...): ", "app-level token", "xapp-")
	if err != nil {
		return nil, err
	}
	creds := keyring.InternalApp{UserToken: user, AppToken: app}
	id, err := internalapp.AuthTest(a.ctx, creds, internalapp.WithAPIURL(a.env.SlackAPIURL))
	if err != nil {
		return nil, err
	}
	creds.WorkspaceURL = id.URL
	return []newWorkspace{{team: id.Team, name: id.Name, creds: creds}}, nil
}

// askToken reads a secret and checks its prefix, without ever printing it.
func (a adder) askToken(prompt, what, prefix string) (string, error) {
	token, err := a.con.AskSecret(prompt)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(token, prefix) {
		return "", fmt.Errorf("the %s must start with %s", what, prefix)
	}
	return token, nil
}

// desktopSession asks for consent, then adds workspaces from the desktop
// app or, when there is none or the user asks, from a browser session.
func (a adder) desktopSession(browser bool) ([]newWorkspace, error) {
	fmt.Fprint(a.env.Stdout, termsWarning)
	answer, err := a.con.Ask(`Type "yes" to accept and continue: `)
	if err != nil {
		return nil, err
	}
	if answer != "yes" {
		fmt.Fprint(a.env.Stdout, declinedHelp)
		return nil, errNotAccepted
	}
	if browser {
		return a.browserSession()
	}
	listed, err := a.env.DesktopWorkspaces(a.ctx)
	if err != nil {
		fmt.Fprintf(a.env.Stdout, "Cannot use the Slack desktop app: %v\n", err)
		return a.browserSession()
	}
	return a.pickDesktop(listed)
}

// pickDesktop lists the desktop app's workspaces and adds the ones the
// user picks. One that needs sign-in is added from a browser session.
func (a adder) pickDesktop(listed []auth.Workspace) ([]newWorkspace, error) {
	fmt.Fprintln(a.env.Stdout, "Workspaces in the Slack desktop app:")
	for i, w := range listed {
		line := fmt.Sprintf("  %d. %s (%s)", i+1, w.Name, w.Team)
		if w.NeedsSignIn() {
			line += ": needs sign-in (pick it to paste a browser session)"
		}
		fmt.Fprintln(a.env.Stdout, line)
	}
	answer, err := a.con.Ask(`Add which? Numbers, "all", or "b" to paste a browser session instead [all]: `)
	if err != nil {
		return nil, err
	}
	if answer == "b" {
		return a.browserSession()
	}
	picked, err := pick(listed, answer)
	if err != nil {
		return nil, err
	}
	added := make([]newWorkspace, len(picked))
	for i, w := range picked {
		if added[i], err = a.fromDesktop(w); err != nil {
			return nil, err
		}
	}
	return added, nil
}

// fromDesktop is a picked workspace ready to store: with the desktop app's
// credentials, once Slack accepts them, or, when it needs sign-in, with a
// browser session pasted for it, which must belong to that workspace.
func (a adder) fromDesktop(w auth.Workspace) (newWorkspace, error) {
	if !w.NeedsSignIn() {
		return a.checkDesktop(w)
	}
	fmt.Fprintf(a.env.Stdout, "%s (%s) needs sign-in. ", w.Name, w.Team)
	pasted, err := a.pasteSession()
	if err != nil {
		return newWorkspace{}, fmt.Errorf("%s (%s) needs sign-in: %w", w.Name, w.Team, err)
	}
	if pasted.team != w.Team {
		return newWorkspace{}, fmt.Errorf("the pasted session is for %s (%s), not %s (%s)", pasted.name, pasted.team, w.Name, w.Team)
	}
	return pasted, nil
}

// checkDesktop asks Slack whether the desktop app's session for w works,
// as pasteSession does for a browser one: the files on disk can hold a
// session Slack no longer accepts. Slack's name and address for the
// workspace win over the ones the app stored, when Slack gives them.
func (a adder) checkDesktop(w auth.Workspace) (newWorkspace, error) {
	id, err := desktopsession.AuthTest(a.env.SlackAPIURL)(a.ctx, w.Credentials)
	if err != nil {
		return newWorkspace{}, explainRejected(fmt.Errorf("%s (%s): %w", w.Name, w.Team, err), desktopRejectedHelp)
	}
	creds := w.Credentials
	creds.WorkspaceURL = cmp.Or(id.URL, creds.WorkspaceURL)
	return newWorkspace{team: w.Team, name: cmp.Or(id.Team, w.Name), creds: creds}, nil
}

// explainRejected adds help to an error that says Slack rejected a
// session just read or pasted, and returns any other error as it is.
func explainRejected(err error, help string) error {
	if !errors.Is(err, provider.ErrAuthExpired) {
		return err
	}
	return fmt.Errorf("%w. Nothing was stored.\n%s", err, help)
}

// pick returns the workspaces an answer names: numbers separated by
// spaces or commas, or every signed-in one for "all" or nothing.
func pick(listed []auth.Workspace, answer string) ([]auth.Workspace, error) {
	if answer == "" || answer == "all" {
		return signedIn(listed)
	}
	var picked []auth.Workspace
	for _, f := range strings.FieldsFunc(answer, func(r rune) bool { return r == ' ' || r == ',' }) {
		w, err := pickOne(listed, f)
		if err != nil {
			return nil, err
		}
		picked = append(picked, w)
	}
	return picked, nil
}

// pickOne returns the workspace numbered n in the list.
func pickOne(listed []auth.Workspace, n string) (auth.Workspace, error) {
	i, err := strconv.Atoi(n)
	if err != nil || i < 1 || i > len(listed) {
		return auth.Workspace{}, fmt.Errorf("no workspace %q", n)
	}
	return listed[i-1], nil
}

func signedIn(listed []auth.Workspace) ([]auth.Workspace, error) {
	var out []auth.Workspace
	for _, w := range listed {
		if !w.NeedsSignIn() {
			out = append(out, w)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no workspace to add: every one needs sign-in; pick one by number to paste a browser session for it")
	}
	return out, nil
}

// browserSession adds the workspace of a browser session.
func (a adder) browserSession() ([]newWorkspace, error) {
	w, err := a.pasteSession()
	if err != nil {
		return nil, err
	}
	return []newWorkspace{w}, nil
}

// pasteSession reads a browser's "Copy as cURL" (or a token and then a
// cookie) and asks Slack which workspace it belongs to.
func (a adder) pasteSession() (newWorkspace, error) {
	fmt.Fprint(a.env.Stdout, browserSteps)
	paste, err := a.con.AskPaste("cURL command or xoxc- token: ")
	if err != nil {
		return newWorkspace{}, err
	}
	if _, err := auth.ParseCurl(paste); errors.Is(err, auth.ErrNoCookie) {
		cookie, err := a.con.AskPaste("d cookie (DevTools > Application > Cookies, xoxd-...): ")
		if err != nil {
			return newWorkspace{}, err
		}
		paste += "\n" + cookie
	}
	w, err := auth.FromCurl(a.ctx, paste, desktopsession.AuthTest(a.env.SlackAPIURL))
	if err != nil {
		return newWorkspace{}, explainRejected(err, boundSessionHelp)
	}
	return newWorkspace{team: w.Team, name: w.Name, creds: w.Credentials}, nil
}
