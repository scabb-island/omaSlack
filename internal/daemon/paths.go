package daemon

import (
	"errors"
	"path/filepath"

	"gitlab.com/scabb-island/omaslack/internal/rpcclient"
)

// ErrNoRuntimeDir means $XDG_RUNTIME_DIR is unset, so there is nowhere to
// put the socket.
var ErrNoRuntimeDir = errors.New("XDG_RUNTIME_DIR is not set; run the daemon from a login session or as the omaslack systemd user service")

// Paths is where the daemon keeps what it owns on disk.
type Paths struct {
	Socket string // the RPC socket, $XDG_RUNTIME_DIR/omaslack.sock
	Data   string // one SQLite database per workspace: <Data>/<team>.db
}

// DefaultPaths resolves Paths from the environment, read through getenv
// (os.Getenv): the socket in $XDG_RUNTIME_DIR, the stores in
// $XDG_DATA_HOME/omaslack, which defaults to ~/.local/share/omaslack.
func DefaultPaths(getenv func(string) string) (Paths, error) {
	runtime := getenv("XDG_RUNTIME_DIR")
	if runtime == "" {
		return Paths{}, ErrNoRuntimeDir
	}
	data := getenv("XDG_DATA_HOME")
	if data == "" {
		home := getenv("HOME")
		if home == "" {
			return Paths{}, errors.New("neither XDG_DATA_HOME nor HOME is set; there is nowhere to keep the stores")
		}
		data = filepath.Join(home, ".local", "share")
	}
	return Paths{
		Socket: filepath.Join(runtime, rpcclient.SocketName),
		Data:   filepath.Join(data, "omaslack"),
	}, nil
}
