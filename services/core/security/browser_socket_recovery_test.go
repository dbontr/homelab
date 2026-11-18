package security

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func testBrowserSocket(t *testing.T, path string) *net.UnixListener {
	t.Helper()
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	socket := listener.(*net.UnixListener)
	socket.SetUnlinkOnClose(false)
	if err := os.Chmod(path, 0660); err != nil {
		_ = socket.Close()
		t.Fatal(err)
	}
	return socket
}

func TestReclaimStaleBrowserSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "browser.sock")
	socket := testBrowserSocket(t, path)
	if err := socket.Close(); err != nil {
		t.Fatal(err)
	}
	if err := reclaimStaleBrowserSocket(path, os.Getegid()); err != nil {
		t.Fatalf("orphaned socket should be reclaimed: %v", err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("orphaned socket pathname remains: %v", err)
	}
	if err := reclaimStaleBrowserSocket(path, os.Getegid()); err != nil {
		t.Fatalf("absent socket must be accepted: %v", err)
	}
}

func TestReclaimBrowserSocketRefusesLiveListener(t *testing.T) {
	path := filepath.Join(t.TempDir(), "browser.sock")
	socket := testBrowserSocket(t, path)
	defer socket.Close()
	if err := reclaimStaleBrowserSocket(path, os.Getegid()); err == nil {
		t.Fatal("an active browser socket was replaced")
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatal("the live socket pathname was changed:", err)
	}
}

func TestReclaimBrowserSocketRejectsUnsafeEntries(t *testing.T) {
	t.Run("ordinary file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "browser.sock")
		if err := os.WriteFile(path, []byte("untrusted"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := reclaimStaleBrowserSocket(path, os.Getegid()); err == nil {
			t.Fatal("an ordinary file was accepted as a browser socket")
		}
	})
	t.Run("symbolic link", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "browser.sock")
		if err := os.Symlink("/tmp/not-a-browser-socket", path); err != nil {
			t.Fatal(err)
		}
		if err := reclaimStaleBrowserSocket(path, os.Getegid()); err == nil {
			t.Fatal("a symbolic link was accepted as a browser socket")
		}
	})
	t.Run("world-accessible socket", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "browser.sock")
		socket := testBrowserSocket(t, path)
		if err := socket.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0666); err != nil {
			t.Fatal(err)
		}
		if err := reclaimStaleBrowserSocket(path, os.Getegid()); err == nil {
			t.Fatal("an unsafe socket mode was accepted")
		}
		if _, err := os.Lstat(path); err != nil {
			t.Fatal("the unsafe socket pathname was changed:", err)
		}
	})
}
