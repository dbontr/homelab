package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/dbontr/homelab-new/system/compute/internal/controller"
)

var programID = regexp.MustCompile("^[a-z0-9][a-z0-9_-]{0,79}$")

func readCatalog(path string) ([]controller.ProgramProposal, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	dec := json.NewDecoder(io.LimitReader(file, 1<<20))
	dec.DisallowUnknownFields()
	var programs []controller.ProgramProposal
	if err := dec.Decode(&programs); err != nil {
		return nil, fmt.Errorf("invalid program catalog: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return nil, errors.New("program catalog contains extra JSON data")
	}
	if len(programs) == 0 || len(programs) > 100 {
		return nil, errors.New("program catalog must have between 1 and 100 entries")
	}
	seen := map[string]bool{}
	for _, p := range programs {
		parsed, err := url.Parse(p.SourceURL)
		if !programID.MatchString(p.ID) || seen[p.ID] ||
			strings.TrimSpace(p.Name) == "" || len(p.Name) > 128 ||
			strings.TrimSpace(p.Version) == "" || len(p.Version) > 64 ||
			len(p.Description) > 1024 || strings.TrimSpace(p.LicenseNotes) == "" ||
			len(p.LicenseNotes) > 1024 || len(p.SourceURL) > 512 ||
			err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" ||
			parsed.User != nil || parsed.Fragment != "" {
			return nil, fmt.Errorf("invalid program catalog entry %q", p.ID)
		}
		switch p.Platform {
		case "linux-container":
			if p.Distribution != "oci" && p.Distribution != "apt" {
				return nil, fmt.Errorf("invalid distribution for %s", p.ID)
			}
		case "windows-native", "macos-native":
			if p.Distribution != "native" {
				return nil, fmt.Errorf("invalid native distribution for %s", p.ID)
			}
		default:
			return nil, fmt.Errorf("invalid execution platform for %s", p.ID)
		}
		seen[p.ID] = true
	}
	return programs, nil
}

func controllerURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" ||
		u.Fragment != "" || (u.Path != "" && u.Path != "/") ||
		(u.Scheme != "https" && !(u.Scheme == "http" &&
			(u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"))) {
		return "", errors.New("controller URL must be HTTPS or a local loopback HTTP listener")
	}
	return strings.TrimRight(u.String(), "/") + "/api/pool/programs", nil
}

func readToken(path string) (string, error) {
	if path == "" {
		return "", errors.New("a protected token file is required for apply")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return "", errors.New("credential must be a readable private regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", errors.New("cannot read protected credential")
	}
	token := strings.TrimSpace(string(data))
	if len(token) < 32 || strings.ContainsAny(token, "\r\n") {
		return "", errors.New("invalid protected credential")
	}
	return token, nil
}

func call(ctx context.Context, client *http.Client, method, endpoint, token string, body any) ([]byte, error) {
	var input io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		input = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, input)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("controller request failed: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("controller rejected catalog request (HTTP %d)", res.StatusCode)
	}
	return io.ReadAll(io.LimitReader(res.Body, 2<<20))
}

func reconcile(ctx context.Context, client *http.Client, endpoint, token string, catalog []controller.ProgramProposal) (int, int, error) {
	body, err := call(ctx, client, http.MethodGet, endpoint, token, nil)
	if err != nil {
		return 0, 0, err
	}
	var existing []controller.ProgramProposal
	if err := json.Unmarshal(body, &existing); err != nil {
		return 0, 0, errors.New("controller returned an invalid program inventory")
	}
	index := make(map[string]controller.ProgramProposal, len(existing))
	for _, p := range existing {
		index[p.ID] = p
	}
	pending := make([]controller.ProgramProposal, 0, len(catalog))
	skipped := 0
	// Check every identity before writing so a conflicting catalog fails closed.
	for _, p := range catalog {
		old, found := index[p.ID]
		if !found {
			pending = append(pending, p)
			continue
		}
		if old.Name != p.Name || old.Version != p.Version ||
			old.Platform != p.Platform || old.Distribution != p.Distribution ||
			old.SourceURL != p.SourceURL {
			return 0, 0, fmt.Errorf("conflicting existing program identity: %s", p.ID)
		}
		skipped++
	}
	added := 0
	for _, p := range pending {
		if _, err := call(ctx, client, http.MethodPost, endpoint, token, p); err != nil {
			return added, skipped, fmt.Errorf("cannot propose %s: %w", p.ID, err)
		}
		added++
	}
	return added, skipped, nil
}

func run() error {
	path := flag.String("catalog", "catalog/engineering-programs.json", "source-owned program catalog JSON")
	apply := flag.Bool("apply", false, "submit missing program requests to an authorized controller")
	base := flag.String("controller-url", "", "authorized controller URL for apply")
	tokenPath := flag.String("token-file", "", "protected controller owner credential file for apply")
	flag.Parse()
	catalog, err := readCatalog(*path)
	if err != nil {
		return err
	}
	if !*apply {
		fmt.Printf("Validated %d versioned program requests; no controller changes made.\n", len(catalog))
		return nil
	}
	endpoint, err := controllerURL(*base)
	if err != nil {
		return err
	}
	token, err := readToken(*tokenPath)
	if err != nil {
		return err
	}
	client := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("controller redirect refused")
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	added, skipped, err := reconcile(ctx, client, endpoint, token, catalog)
	if err != nil {
		return err
	}
	fmt.Printf("Registered %d requests; preserved %d existing program records and their review states.\n", added, skipped)
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
