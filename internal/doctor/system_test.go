package doctor

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/rpc"
	"gitlab.com/scabb-island/omaslack/internal/rpcclient"
	"gitlab.com/scabb-island/omaslack/internal/version"
)

// fakeOutput answers each command line (name and args joined by spaces)
// with out, or fails for the ones in errs.
func fakeOutput(out map[string]string, errs map[string]error) Output {
	return func(_ context.Context, name string, args ...string) (string, error) {
		line := strings.Join(append([]string{name}, args...), " ")
		if err, ok := errs[line]; ok {
			return "", err
		}
		return out[line], nil
	}
}

const showUnit = "systemctl --user show omaslack --property=LoadState,ActiveState,UnitFileState"

func TestServiceReadsSystemctlShow(t *testing.T) {
	run := fakeOutput(map[string]string{showUnit: "LoadState=loaded\nActiveState=active\nUnitFileState=enabled\nOther=x\n"}, nil)
	u, err := service(context.Background(), run)
	if want := (Unit{Load: "loaded", Active: "active", UnitFile: "enabled"}); err != nil || u != want {
		t.Fatalf("service = %+v, %v; want %+v", u, err, want)
	}
	_, err = service(context.Background(), fakeOutput(nil, map[string]error{showUnit: errors.New("no bus")}))
	if err == nil {
		t.Fatal("a failing systemctl: want an error")
	}
}

func TestPluginsReadsOmarchyPluginList(t *testing.T) {
	const list = "omarchy plugin list --json"
	run := fakeOutput(map[string]string{list: `[{"id":"omarchy.clock","name":"Clock","enabled":true},{"id":"ar.scabb.omaslack","enabled":false}]`}, nil)
	got, err := plugins(context.Background(), run)
	want := []Plugin{{ID: "omarchy.clock", Enabled: true}, {ID: PluginID}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("plugins = %+v, %v; want %+v", got, err, want)
	}
	if _, err := plugins(context.Background(), fakeOutput(map[string]string{list: "Usage: ..."}, nil)); err == nil {
		t.Error("output that is not JSON: want an error")
	}
	if _, err := plugins(context.Background(), fakeOutput(nil, map[string]error{list: errors.New("no omarchy")})); err == nil {
		t.Error("a failing omarchy: want an error")
	}
}

func TestShellPingsAndReadsTheVersion(t *testing.T) {
	found := func(string) (string, error) { return "/usr/bin/omarchy-shell", nil }
	run := fakeOutput(map[string]string{"omarchy-shell shell ping": "ok\n", "omarchy-version": "4.5.0-3\n"}, nil)
	if s, err := shell(context.Background(), found, run); err != nil || s != (Shell{Running: true, Version: "4.5.0-3"}) {
		t.Errorf("shell = %+v, %v", s, err)
	}
	down := fakeOutput(nil, map[string]error{"omarchy-shell shell ping": errors.New("no shell"), "omarchy-version": errors.New("x")})
	if s, err := shell(context.Background(), found, down); err != nil || s != (Shell{}) {
		t.Errorf("shell not answering = %+v, %v", s, err)
	}
	missing := func(string) (string, error) { return "", errors.New("not found") }
	if _, err := shell(context.Background(), missing, run); err == nil {
		t.Error("no omarchy-shell: want an error")
	}
}

func TestStatReadsModeAndOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hook")
	if err := os.WriteFile(path, nil, 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := stat(path)
	if err != nil || f.Mode.Perm() != 0o700 || f.UID != os.Getuid() {
		t.Errorf("stat = %+v, %v", f, err)
	}
	if _, err := stat(path + ".missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing file: err = %v, want fs.ErrNotExist", err)
	}
}

func TestDialHelloDoesNotStartTheDaemon(t *testing.T) {
	_, err := dialHello(context.Background(), filepath.Join(t.TempDir(), "omaslack.sock"))
	if !errors.Is(err, rpcclient.ErrUnavailable) || !errors.Is(err, errNotStarting) {
		t.Fatalf("err = %v, want ErrUnavailable without starting the daemon", err)
	}
}

func TestDialHelloReadsTheDaemonsAnswer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "omaslack.sock")
	l, err := rpc.Listen(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	srv := rpc.NewServer(rpc.Router{}, rpc.Options{Hello: func(context.Context, rpc.Hello) (map[string]any, error) {
		return map[string]any{"workspaces": []map[string]any{
			{"team": "T0APP", "name": "Acme", "provider": "internal-app", "capabilities": []string{}, "state": "connected"},
		}}, nil
	}})
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Close() })

	h, err := dialHello(context.Background(), path)
	want := Hello{Protocol: version.Protocol, DaemonVersion: version.Version,
		Workspaces: []Workspace{{Team: "T0APP", Provider: "internal-app", State: "connected"}}}
	if err != nil || !reflect.DeepEqual(h, want) {
		t.Fatalf("hello = %+v, %v; want %+v", h, err, want)
	}
}

func TestParseHelloRejectsGarbage(t *testing.T) {
	if _, err := parseHello([]byte(`[1,2]`)); err == nil {
		t.Error("want an error")
	}
}

func TestSystemProbesFollowTheEnvironment(t *testing.T) {
	t.Setenv("HOME", "/home/me")
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	t.Setenv("XDG_CONFIG_HOME", "/home/me/.config")
	p := SystemProbes(nil)
	want := PathsFor("/home/me", "/run/user/1000")
	want.CredentialsFile = "/home/me/.config/omaslack/credentials"
	if p.Paths != want || p.UID != os.Getuid() {
		t.Errorf("paths = %+v, uid %d; want %+v", p.Paths, p.UID, want)
	}
}
