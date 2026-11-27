package cli

import (
	"context"
	"errors"
	"fmt"

	"gitlab.com/scabb-island/omaslack/internal/doctor"
)

// doctorUsage is the usage of `omaslack doctor`.
const doctorUsage = `Usage: omaslack doctor

Checks the daemon and its protocol, the user service, the socket, where the
credentials live, each workspace's provider, the Omarchy theme template and
hook, the plugin and the shell. Prints OK, WARN or FAIL for each, with the
command that fixes it. It changes nothing: it does not even start the daemon.

Exit codes:
  0  no check failed (warnings allowed)
  1  at least one check failed
  2  usage error
`

// errChecksFailed is what doctor returns when a check failed.
var errChecksFailed = errors.New("some checks failed")

func runDoctor(env Env, args []string) error {
	if len(args) > 0 {
		return &usageError{err: errors.New("doctor takes no arguments"), usage: doctorUsage}
	}
	probes := env.DoctorProbes
	if probes == nil {
		probes = func() doctor.Probes { return doctor.SystemProbes(env.Keyring) }
	}
	results := doctor.Run(context.Background(), probes())
	if err := doctor.Report(env.Stdout, results); err != nil {
		return fmt.Errorf("print the report: %w", err)
	}
	if doctor.Failed(results) {
		return errChecksFailed
	}
	return nil
}
