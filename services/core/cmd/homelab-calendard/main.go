// homelab-calendard provides the read-only calendar feed on a private listener.
package main

import (
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dbontr/homelab-new/system/internal/calendarhttp"
)

func readCredential(name string) (string, error) {
	path := os.Getenv(name)
	if !filepath.IsAbs(path) {
		return "", errors.New("calendar credential requires an absolute protected path")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(string(raw), "\n"), nil
}

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)
	feedURL, err := readCredential("HOMELAB_CALENDAR_URL_FILE")
	if err != nil {
		log.Fatal("calendar feed configuration: ", err)
	}
	token, err := readCredential("HOMELAB_CALENDAR_TOKEN_FILE")
	if err != nil {
		log.Fatal("calendar service identity: ", err)
	}
	listen := os.Getenv("HOMELAB_CALENDAR_LISTEN_ADDR")
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		log.Fatal("calendar listener must use a loopback address")
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		log.Fatal("calendar listener must use a literal loopback IP")
	}
	client := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("external calendar redirects are disabled")
		},
	}
	handler, err := calendarhttp.NewHandler(feedURL, token, client)
	if err != nil {
		log.Fatal("calendar service configuration: ", err)
	}
	server := &http.Server{
		Addr:              listen,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       25 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}
	log.Print("calendar service listening on a private interface")
	log.Fatal(server.ListenAndServe())
}
