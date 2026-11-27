package theme

import (
	"context"
	"errors"
	"os"
	"syscall"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
)

func recv(t *testing.T, ch <-chan Theme) (Theme, bool) {
	t.Helper()
	select {
	case th, ok := <-ch:
		return th, ok
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting on Watch")
		return Theme{}, false
	}
}

func TestWatchSendsGoodThemesAndSkipsFailures(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigs := make(chan os.Signal)
	results := []error{nil, errors.New("broken colors.toml"), nil}
	calls := 0
	load := func() (Theme, error) {
		err := results[calls]
		calls++
		th := Default()
		th.Accent = lipgloss.Color([]string{"#000001", "#000002", "#000003"}[calls-1])
		return th, err
	}
	ch := Watch(ctx, load, sigs)

	sigs <- syscall.SIGUSR1
	if th, _ := recv(t, ch); hex(th.Accent) != "#000001" {
		t.Fatalf("first reload accent = %s", hex(th.Accent))
	}
	sigs <- syscall.SIGUSR1 // fails: nothing is sent
	sigs <- syscall.SIGUSR1
	if th, _ := recv(t, ch); hex(th.Accent) != "#000003" {
		t.Fatalf("third reload accent = %s", hex(th.Accent))
	}

	cancel()
	if _, ok := recv(t, ch); ok {
		t.Fatal("channel must close when ctx ends")
	}
}

func TestWatchClosesWhileASendIsPending(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	sigs := make(chan os.Signal, 1)
	ch := Watch(ctx, func() (Theme, error) { return Default(), nil }, sigs)
	sigs <- syscall.SIGUSR1
	time.Sleep(10 * time.Millisecond) // let Watch block on the unread send
	cancel()
	for range ch { //nolint:revive // drain until closed
	}
}
