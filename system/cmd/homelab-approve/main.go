package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"
)

// Execute from a trusted administrator shell. Never paste passwords into chat.
func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: homelab-approve list | approve ID")
		os.Exit(2)
	}
	socket := os.Getenv("HOMELAB_APPROVAL_SOCKET")
	if socket == "" {
		socket = "/run/homelab-platform/approval.sock"
	}
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		}},
	}
	method, endpoint := http.MethodGet, "http://unix/approvals"
	if os.Args[1] == "approve" && len(os.Args) == 3 {
		method = http.MethodPost
		endpoint += "/" + os.Args[2]
	} else if os.Args[1] != "list" || len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "invalid arguments")
		os.Exit(2)
	}
	request, err := http.NewRequest(method, endpoint, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	response, err := client.Do(request)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 128<<10))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(string(body))
	if response.StatusCode != http.StatusOK {
		os.Exit(1)
	}
}
