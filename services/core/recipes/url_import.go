package recipes

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

const remoteRecipeLimit = 512 << 10

// Addresses that can expose internal services or do not represent ordinary
// public Internet endpoints are not eligible as recipe-import destinations.
var reservedRecipeNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/96"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

func publicRecipeAddress(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || !addr.IsGlobalUnicast() || addr.IsPrivate() ||
		addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range reservedRecipeNetworks {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

func recipeURL(raw string) (*url.URL, error) {
	if len(raw) == 0 || len(raw) > 2000 || strings.TrimSpace(raw) != raw {
		return nil, errors.New("recipe URL must be 1-2000 characters")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Opaque != "" ||
		u.User != nil || u.Hostname() == "" || u.Fragment != "" {
		return nil, errors.New("recipe URL must be an HTTPS address without credentials or fragments")
	}
	if port := u.Port(); port != "" && port != "443" {
		return nil, errors.New("recipe URLs may use only HTTPS port 443")
	}
	host := u.Hostname()
	if net.ParseIP(host) != nil || len(host) > 253 || !strings.Contains(host, ".") {
		return nil, errors.New("recipe URL requires a public DNS hostname")
	}
	for _, label := range strings.Split(strings.ToLower(host), ".") {
		if len(label) == 0 || len(label) > 63 ||
			label[0] == '-' || label[len(label)-1] == '-' {
			return nil, errors.New("invalid recipe hostname")
		}
		for _, character := range label {
			if !((character >= 'a' && character <= 'z') ||
				(character >= '0' && character <= '9') || character == '-') {
				return nil, errors.New("invalid recipe hostname")
			}
		}
	}
	return u, nil
}

func recipeAddresses(ctx context.Context, host string) ([]netip.Addr, error) {
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return nil, errors.New("recipe hostname could not be resolved")
	}
	for _, ip := range ips {
		if !publicRecipeAddress(ip) {
			return nil, errors.New("recipe hostname resolves to a restricted network")
		}
	}
	return ips, nil
}

// FetchSchemaOrgURL retrieves only the document at an owner-supplied HTTPS
// address. DNS is validated once and pinned into the dial operation, with no
// proxy, redirect, or secondary DNS lookup. It never executes page scripts or
// downloads referenced images. The result is a draft, not a saved recipe.
func FetchSchemaOrgURL(ctx context.Context, raw string) (Recipe, error) {
	u, err := recipeURL(raw)
	if err != nil {
		return Recipe{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ips, err := recipeAddresses(ctx, u.Hostname())
	if err != nil {
		return Recipe{}, err
	}
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	transport := &http.Transport{
		Proxy: nil,
		DisableKeepAlives: true,
		TLSHandshakeTimeout: 4 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
		MaxResponseHeaderBytes: 16 << 10,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil || !strings.EqualFold(host, u.Hostname()) || port != "443" {
				return nil, errors.New("unexpected recipe connection destination")
			}
			var last error
			for _, ip := range ips {
				conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), port))
				if err == nil {
					return conn, nil
				}
				last = err
			}
			return nil, last
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		Timeout: 10 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Recipe{}, errors.New("invalid recipe URL")
	}
	request.Header.Set("Accept", "text/html, application/ld+json, application/json")
	request.Header.Set("User-Agent", "HomelabRecipeImporter/1.0")
	response, err := client.Do(request)
	if err != nil {
		return Recipe{}, errors.New("recipe page could not be fetched")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Recipe{}, fmt.Errorf("recipe page returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength > remoteRecipeLimit {
		return Recipe{}, errors.New("recipe page exceeds the 512 KiB import limit")
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || (mediaType != "text/html" && mediaType != "application/ld+json" &&
		mediaType != "application/json") {
		return Recipe{}, errors.New("recipe page is not HTML or JSON-LD")
	}
	document, err := io.ReadAll(io.LimitReader(response.Body, remoteRecipeLimit+1))
	if err != nil {
		return Recipe{}, errors.New("recipe page could not be read")
	}
	if len(document) > remoteRecipeLimit {
		return Recipe{}, errors.New("recipe page exceeds the 512 KiB import limit")
	}
	draft, err := ParseSchemaOrg(string(document))
	if err != nil {
		return Recipe{}, err
	}
	draft.SourceURL = u.String()
	return draft, nil
}
