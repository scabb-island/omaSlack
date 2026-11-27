package auth

// DesktopSourceName is how the Slack desktop app is named as a
// desktop-session source, in the CLI and in omaslack doctor.
const DesktopSourceName = "the Slack desktop app"

// SignedInSources names the desktop-session sources signed in to Slack on
// this machine: the Slack desktop app first, then each Chromium browser.
// It reads only plaintext tokens (SignedIn), so it never prompts for the
// keyring; a name here is not a promise the session still works.
func SignedInSources() []string {
	var names []string
	if dir, err := FindDesktopDir(); err == nil && (Desktop{Dir: dir}).SignedIn() {
		names = append(names, DesktopSourceName)
	}
	for _, b := range FindBrowsers() {
		if b.SignedIn() {
			names = append(names, b.Name)
		}
	}
	return names
}
