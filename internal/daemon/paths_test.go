package daemon

import (
	"errors"
	"testing"
)

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestDefaultPathsFollowTheXDGVariables(t *testing.T) {
	got, err := DefaultPaths(env(map[string]string{
		"XDG_RUNTIME_DIR": "/run/user/1000", "XDG_DATA_HOME": "/data", "HOME": "/home/u",
	}))
	if err != nil {
		t.Fatal(err)
	}
	want := Paths{Socket: "/run/user/1000/omaslack.sock", Data: "/data/omaslack"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestDefaultDataDirIsUnderHome(t *testing.T) {
	got, err := DefaultPaths(env(map[string]string{"XDG_RUNTIME_DIR": "/run/user/1000", "HOME": "/home/u"}))
	if err != nil {
		t.Fatal(err)
	}
	if got.Data != "/home/u/.local/share/omaslack" {
		t.Fatalf("data dir %q", got.Data)
	}
}

func TestDefaultPathsNeedTheRuntimeDir(t *testing.T) {
	_, err := DefaultPaths(env(map[string]string{"HOME": "/home/u"}))
	if !errors.Is(err, ErrNoRuntimeDir) {
		t.Fatalf("err = %v, want ErrNoRuntimeDir", err)
	}
}

func TestDefaultPathsNeedADataDir(t *testing.T) {
	_, err := DefaultPaths(env(map[string]string{"XDG_RUNTIME_DIR": "/run/user/1000"}))
	if err == nil {
		t.Fatal("no error without XDG_DATA_HOME and HOME")
	}
}
