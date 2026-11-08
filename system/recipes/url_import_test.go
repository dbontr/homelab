package recipes

import (
	"context"
	"net/netip"
	"strings"
	"testing"
)

func TestRecipeURLOnlyAcceptsPublicHTTPSHostnames(t *testing.T) {
	for _, address := range []string{
		"http://example.com/recipe",
		"https://localhost/recipe",
		"https://127.0.0.1/recipe",
		"https://[::1]/recipe",
		"https://192.168.2.1/recipe",
		"https://example.com:8443/recipe",
		"https://user:secret@example.com/recipe",
		"https://example.com./recipe",
		"https://example..com/recipe",
		"https://-example.com/recipe",
		"https://example.com/recipe#section",
		"https://example.com@localhost/recipe",
		"https://example.com.evil.invalid:8080/",
		"https://example.com/recipe\n",
	} {
		if _, err := recipeURL(address); err == nil {
			t.Errorf("accepted unsafe recipe URL %q", address)
		}
	}
	for _, address := range []string{
		"https://example.com/recipe?ref=cooking",
		"https://www.example.com:443/recipes/cake",
	} {
		if _, err := recipeURL(address); err != nil {
			t.Errorf("rejected ordinary HTTPS recipe URL %q: %v", address, err)
		}
	}
}

func TestRecipeNetworksRejectInternalAndSpecialRanges(t *testing.T) {
	for _, ip := range []string{
		"0.0.0.0", "10.77.30.229", "100.80.246.102", "127.0.0.1",
		"169.254.169.254", "172.16.0.1", "192.168.0.1",
		"192.0.2.1", "198.19.0.1", "198.51.100.2",
		"203.0.113.4", "224.0.0.1", "::1", "fc00::1",
		"fe80::1", "2001:db8::1", "::ffff:127.0.0.1",
	} {
		if publicRecipeAddress(netip.MustParseAddr(ip)) {
			t.Errorf("allowed restricted destination %s", ip)
		}
	}
	for _, ip := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if !publicRecipeAddress(netip.MustParseAddr(ip)) {
			t.Errorf("blocked public destination %s", ip)
		}
	}
}

func TestFetchSchemaOrgURLRejectsUnsafeInputBeforeNetwork(t *testing.T) {
	_, err := FetchSchemaOrgURL(context.Background(), "https://127.0.0.1/latest/meta-data")
	if err == nil || !strings.Contains(err.Error(), "public DNS hostname") {
		t.Fatalf("unsafe URL was not rejected before DNS: %v", err)
	}
}
