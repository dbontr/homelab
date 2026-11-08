package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/dbontr/homelab-new/system/learning/internal/mcpsvc"
	"github.com/dbontr/homelab-new/system/learning/internal/store"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func getenv(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
func readBearerCredential() (string, error) {
	path := os.Getenv("ADAPTIVE_TEACHING_BEARER_TOKEN_FILE")
	if path == "" {
		return "", nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("bearer token file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return "", errors.New("bearer token file must be regular and owner-only")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read bearer token file: %w", err)
	}
	token := strings.TrimSpace(string(data))
	if len(token) < 32 || strings.ContainsAny(token, "\r\n\x00") {
		return "", errors.New("bearer token must contain at least 32 characters on one line")
	}
	return token, nil
}

func loopbackListener(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	return host == "localhost" || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "backup" {
		flags := flag.NewFlagSet("backup", flag.ExitOnError)
		source := flags.String("db", getenv("ADAPTIVE_TEACHING_DB_PATH", "./data/learning.db"), "existing learning database")
		output := flags.String("output", "", "destination snapshot path (must not exist)")
		if err := flags.Parse(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		if *output == "" || flags.NArg() != 0 {
			log.Fatal("usage: adaptive-teaching backup --db <database> --output <snapshot>")
		}
		if _, err := os.Stat(*source); err != nil {
			log.Fatal("source database must exist: ", err)
		}
		database, err := store.Open(*source)
		if err != nil {
			log.Fatal(err)
		}
		defer database.Close()
		if err := database.Backup(*output); err != nil {
			log.Fatal(err)
		}
		fmt.Println("SQLite snapshot written:", *output)
		return
	}
	db, err := store.Open(getenv("ADAPTIVE_TEACHING_DB_PATH", "./data/learning.db"))
	if err != nil {
		log.Fatal("database: ", err)
	}
	defer db.Close()
	svc := mcpsvc.NewService(db)
	addr := getenv("ADAPTIVE_TEACHING_LISTEN_ADDR", "127.0.0.1:8000")
	token, err := readBearerCredential()
	if err != nil {
		log.Fatal(err)
	}
	access, err := mcpsvc.AccessFromEnv()
	if err != nil {
		log.Fatal("Access configuration: ", err)
	}
	if !loopbackListener(addr) && token == "" {
		log.Fatal("non-loopback listener requires ADAPTIVE_TEACHING_BEARER_TOKEN_FILE")
	}
	server := &http.Server{Addr: addr, Handler: svc.HandlerWithAccess(getenv("ADAPTIVE_TEACHING_ALLOWED_HOSTS", "127.0.0.1:*,localhost:*"), os.Getenv("ADAPTIVE_TEACHING_ALLOWED_ORIGINS"), token, access), ReadHeaderTimeout: 10 * time.Second, MaxHeaderBytes: 1 << 20}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	shutdownFinished := make(chan struct{})
	go func() {
		defer close(shutdownFinished)
		<-ctx.Done()
		deadline, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(deadline); err != nil {
			log.Printf("HTTP shutdown: %v", err)
		}
	}()
	log.Printf("Adaptive Teaching MCP listening on %s", addr)
	serveErr := server.ListenAndServe()
	stop()
	<-shutdownFinished
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		log.Fatal(serveErr)
	}
}
