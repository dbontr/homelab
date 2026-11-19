package transport

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

const testArtifactID = "0123456789abcdef0123456789abcdef"

func encodedArtifact(t *testing.T, id string, data []byte) []byte {
	t.Helper()
	hash := sha256.Sum256(data)
	payload, err := json.Marshal(map[string]any{
		"id":          id,
		"name":        "test.txt",
		"size":        len(data),
		"sha256":      hex.EncodeToString(hash[:]),
		"data_base64": base64.StdEncoding.EncodeToString(data),
	})
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestBrowserProjectArtifactPathsAreExact(t *testing.T) {
	valid := "/v1/engineering/projects/homelab-platform/artifacts/" + testArtifactID + "/download"
	if !projectArtifactDownloadPath(valid) {
		t.Fatal("legitimate owner artifact download path denied")
	}
	for _, path := range []string{
		valid + "/extra",
		"/v1/engineering/projects/homelab-platform/artifacts/" + strings.Repeat("g", 32) + "/download",
		"/v1/engineering/projects/HOMELAB/artifacts/" + testArtifactID + "/download",
		"/v1/engineering/projects/homelab-platform/artifacts/" + testArtifactID,
		"/v1/engineering/projects/homelab-platform/artifacts/" + testArtifactID + "/delete",
		"/v1/engineering/projects/homelab-platform/artifacts/" + testArtifactID + "/download?extra",
		"/v1/engineering/projects/homelab-platform/artifacts/../download",
	} {
		if projectArtifactDownloadPath(path) {
			t.Fatalf("invalid path admitted: %q", path)
		}
	}
}

func TestDecodeBrowserArtifactChecksExactDigestAndSize(t *testing.T) {
	data := []byte("verified project output\n")
	body := encodedArtifact(t, testArtifactID, data)
	decoded, filename, err := decodeBrowserArtifact(body, testArtifactID)
	if err != nil || string(decoded) != string(data) || filename != "test.txt" {
		t.Fatalf("verified artifact rejected: %s %q %v", string(decoded), filename, err)
	}
	if _, _, err := decodeBrowserArtifact(body, strings.Repeat("f", 32)); err == nil {
		t.Fatal("cross-artifact response accepted")
	}
	changes := []func(map[string]any){
		func(x map[string]any) { x["size"] = len(data) + 1 },
		func(x map[string]any) { x["sha256"] = strings.Repeat("f", 64) },
		func(x map[string]any) { x["data_base64"] = "AQ=" },
		func(x map[string]any) { x["data_base64"] = "!" },
		func(x map[string]any) { x["size"] = 512*1024 + 1 },
		func(x map[string]any) { x["id"] = strings.Repeat("a", 32) },
	}
	for _, change := range changes {
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
		change(payload)
		altered, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := decodeBrowserArtifact(altered, testArtifactID); err == nil {
			t.Fatalf("corrupted artifact response accepted: %s", altered)
		}
	}
}

func TestDecodeBrowserArtifactSanitizesDownloadName(t *testing.T) {
	for _, name := range []string{"../unauthorized.txt", "unsafe\\path", "unsafe\r\nName", strings.Repeat("x", 121)} {
		var payload map[string]any
		if err := json.Unmarshal(encodedArtifact(t, testArtifactID, []byte("body")), &payload); err != nil {
			t.Fatal(err)
		}
		payload["name"] = name
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		_, filename, err := decodeBrowserArtifact(raw, testArtifactID)
		if err != nil || filename != testArtifactID+".bin" {
			t.Fatalf("unsafe name accepted: %q => %q %v", name, filename, err)
		}
	}
}
