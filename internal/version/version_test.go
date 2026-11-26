package version

import "testing"

func TestStringNamesEveryBuildField(t *testing.T) {
	Version, Commit, Date = "1.2.3", "abc1234", "2026-11-26T00:00:00Z"
	t.Cleanup(func() { Version, Commit, Date = "dev", "none", "unknown" })

	want := "omaslack 1.2.3 (commit abc1234, built 2026-11-26T00:00:00Z, protocol 1)"
	if got := String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
