package rpc

import (
	"os"
	"regexp"
	"strconv"
	"testing"
)

// The QML plugin stops retrying when hello answers this code
// (plugin/lib/rpc.js INCOMPATIBLE), so both sides must use the same one.
func TestThePluginKnowsTheIncompatibleProtocolCode(t *testing.T) {
	src, err := os.ReadFile("../../plugin/lib/rpc.js")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^var INCOMPATIBLE = (-?\d+)$`).FindSubmatch(src)
	if m == nil {
		t.Fatal("plugin/lib/rpc.js has no `var INCOMPATIBLE = <n>` line")
	}
	if got, _ := strconv.Atoi(string(m[1])); Code(got) != CodeIncompatibleProtocol {
		t.Fatalf("plugin/lib/rpc.js INCOMPATIBLE = %d, want %d", got, CodeIncompatibleProtocol)
	}
}
