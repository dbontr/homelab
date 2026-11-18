package security

import (
	"errors"
	"net"
	"os"
	"syscall"
	"time"
)

// reclaimStaleBrowserSocket permits recovery across a security-authority
// executable switch without taking over a live listener. The security state
// lease excludes another authority, and the browser socket directory is
// writable only by the authority's service identity.
func reclaimStaleBrowserSocket(path string, groupID int) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	inode, ok := info.Sys().(*syscall.Stat_t)
	if !ok || info.Mode()&os.ModeType != os.ModeSocket ||
		inode.Uid != uint32(os.Geteuid()) ||
		inode.Gid != uint32(groupID) ||
		inode.Nlink != 1 ||
		info.Mode().Perm()&0007 != 0 {
		return errors.New("browser socket path has unsafe custody")
	}

	conn, dialErr := net.DialTimeout("unix", path, 250*time.Millisecond)
	if dialErr == nil {
		_ = conn.Close()
		return errors.New("browser socket already has a listener")
	}
	// A refused Unix-domain connection establishes that no process is
	// listening at this socket. Timeout and permission errors remain denials.
	if !errors.Is(dialErr, syscall.ECONNREFUSED) {
		return errors.New("browser socket liveness cannot be established")
	}

	again, err := os.Lstat(path)
	if err != nil {
		return err
	}
	recheck, ok := again.Sys().(*syscall.Stat_t)
	if !ok || again.Mode()&os.ModeType != os.ModeSocket ||
		recheck.Dev != inode.Dev || recheck.Ino != inode.Ino {
		return errors.New("browser socket changed during recovery")
	}
	return os.Remove(path)
}
