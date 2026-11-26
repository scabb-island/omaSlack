package rpc

import (
	"errors"
	"net"
	"os"
	"testing"
)

// accepted returns the server side of a fresh connection to a new socket.
func accepted(t *testing.T) *net.UnixConn {
	t.Helper()
	path := socketPath(t)
	l := listen(t, path)
	t.Cleanup(func() { _ = l.Close() })
	dial(t, path)
	c, err := l.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestCheckPeerAllowsTheOwner(t *testing.T) {
	if err := checkPeer(accepted(t), uint32(os.Getuid())); err != nil {
		t.Fatalf("checkPeer = %v", err)
	}
}

func TestCheckPeerRejectsAnotherUser(t *testing.T) {
	err := checkPeer(accepted(t), uint32(os.Getuid())+1)
	if !errors.Is(err, errForeignPeer) {
		t.Fatalf("checkPeer = %v, want errForeignPeer", err)
	}
}

func TestCheckPeerFailsOnAClosedConnection(t *testing.T) {
	c := accepted(t)
	_ = c.Close()
	if err := checkPeer(c, uint32(os.Getuid())); err == nil {
		t.Fatal("checkPeer succeeded on a closed connection")
	}
}

func TestCheckPeerFailsWithoutASocket(t *testing.T) {
	if err := checkPeer(&net.UnixConn{}, uint32(os.Getuid())); err == nil {
		t.Fatal("checkPeer succeeded without a socket")
	}
}
