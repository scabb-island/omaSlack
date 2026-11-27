// Spike: exercise the production browser-session reader against the real
// browser profiles on this machine and validate each session with Slack's
// auth.test. Throwaway live check for add-browser-session-login; the unit
// tests cover the logic with fixtures.
//
// Run: go run ./spikes/browser-session
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/provider/desktopsession"
	"gitlab.com/scabb-island/omaslack/internal/provider/desktopsession/auth"
)

func main() {
	browsers := auth.FindBrowsers()
	if len(browsers) == 0 {
		fmt.Println("No Chromium browser profiles found.")
		return
	}
	for _, b := range browsers {
		fmt.Printf("== %s (%s)\n", b.Name, b.Dir)
		workspaces, err := b.Workspaces(context.Background())
		if err != nil {
			fmt.Printf("   %v\n", err)
			continue
		}
		for _, w := range workspaces {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			id, err := desktopsession.AuthTest(desktopsession.DefaultAPIURL)(ctx, w.Credentials)
			cancel()
			if err != nil {
				fmt.Printf("   %s (%s): REJECTED: %v\n", w.Name, w.Team, err)
				continue
			}
			fmt.Printf("   %s (%s): OK -> team=%q url=%s\n", w.Name, w.Team, id.Team, id.URL)
		}
	}
	_ = os.Stdout
}
