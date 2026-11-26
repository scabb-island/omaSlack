// Command omaslack is the single binary behind the omaslack Slack client:
// daemon, terminal client, control CLI and setup tooling.
package main

import (
	"os"

	"gitlab.com/scabb-island/omaslack/internal/cli"
)

func main() {
	os.Exit(cli.Run(cli.Env{Stdout: os.Stdout, Stderr: os.Stderr}, os.Args[1:]))
}
