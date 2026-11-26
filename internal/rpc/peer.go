package rpc

import (
	"errors"
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

var errForeignPeer = errors.New("peer is another user")

// sameUser is the whole access policy: only the daemon's owner may talk to
// it (spec daemon-api, "Private local socket").
func sameUser(peer, owner uint32) bool { return peer == owner }

// checkPeer returns nil only when SO_PEERCRED says the process on the other
// end runs as owner.
func checkPeer(c *net.UnixConn, owner uint32) error {
	raw, err := c.SyscallConn()
	if err != nil {
		return err
	}
	var cred *unix.Ucred
	var credErr error
	err = raw.Control(func(fd uintptr) {
		cred, credErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	})
	if err := errors.Join(err, credErr); err != nil {
		return err
	}
	if !sameUser(cred.Uid, owner) {
		return fmt.Errorf("%w (uid %d, pid %d)", errForeignPeer, cred.Uid, cred.Pid)
	}
	return nil
}
