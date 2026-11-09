package main

import (
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/dbontr/homelab-new/system/transport"
)

// serveApprovals exposes no TCP listener; access is restricted by directory
// and Unix socket permissions, enforced by the operating system.
func serveApprovals(server *transport.Server, path string) {
	if path == "" {
		return
	}
	if !filepath.IsAbs(path) {
		log.Fatal("approval socket path must be absolute")
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		log.Fatal("approval socket already exists or is inaccessible")
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		_ = listener.Close()
		log.Fatal(err)
	}
	go func() {
		if err := http.Serve(listener, server.AdminHandler()); err != nil {
			log.Print("approval listener stopped: ", err)
		}
	}()
}
