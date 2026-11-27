package version

import (
	"os"
	"regexp"
	"strconv"
	"testing"
)

// The QML plugin says which protocol it speaks in hello (plugin/lib/rpc.js).
// It ships in the same release as the daemon, so the two must agree, or
// every plugin connection is refused as incompatible.
func TestThePluginSpeaksTheDaemonProtocol(t *testing.T) {
	src, err := os.ReadFile("../../plugin/lib/rpc.js")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^var PROTOCOL = (\d+)$`).FindSubmatch(src)
	if m == nil {
		t.Fatal("plugin/lib/rpc.js has no `var PROTOCOL = <n>` line")
	}
	if got, _ := strconv.Atoi(string(m[1])); got != Protocol {
		t.Fatalf("plugin/lib/rpc.js PROTOCOL = %d, want version.Protocol = %d", got, Protocol)
	}
}
