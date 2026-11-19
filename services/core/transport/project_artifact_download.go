package transport

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"regexp"
	"strings"
)

const maxBrowserArtifact = 512 << 10

var browserArtifactID = regexp.MustCompile("^[0-9a-f]{32}$")
var browserArtifactDigest = regexp.MustCompile("^[0-9a-f]{64}$")

// projectArtifactDownloadPath identifies only the project-scoped, read-only
// artifact retrieval route. The same exact path contract is used by session
// authorization and the handler.
func projectArtifactDownloadPath(path string) bool {
	parts := strings.Split(path, "/")
	return len(parts) == 8 &&
		parts[0] == "" && parts[1] == "v1" &&
		parts[2] == "engineering" && parts[3] == "projects" &&
		consoleProjectID.MatchString(parts[4]) &&
		parts[5] == "artifacts" && browserArtifactID.MatchString(parts[6]) &&
		parts[7] == "download"
}

type browserArtifact struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Size       int    `json:"size"`
	SHA256     string `json:"sha256"`
	DataBase64 string `json:"data_base64"`
}

// decodeBrowserArtifact verifies Compute's project-scoped response again at
// the HTTP boundary. No unverified file bytes are returned to the browser.
func decodeBrowserArtifact(raw []byte, id string) ([]byte, string, error) {
	if len(raw) > 1<<20 || !browserArtifactID.MatchString(id) {
		return nil, "", errors.New("invalid artifact response")
	}
	var item browserArtifact
	if err := json.Unmarshal(raw, &item); err != nil ||
		item.ID != id || !browserArtifactDigest.MatchString(item.SHA256) ||
		item.Size < 0 || item.Size > maxBrowserArtifact ||
		len(item.DataBase64) > base64.StdEncoding.EncodedLen(maxBrowserArtifact) {
		return nil, "", errors.New("invalid artifact metadata")
	}
	data, err := base64.StdEncoding.Strict().DecodeString(item.DataBase64)
	if err != nil || len(data) != item.Size {
		return nil, "", errors.New("invalid artifact encoding")
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != item.SHA256 {
		return nil, "", errors.New("artifact digest mismatch")
	}
	name := item.Name
	if name == "" || len(name) > 120 || name == "." || name == ".." ||
		strings.ContainsAny(name, "/\\\r\n") {
		name = id + ".bin"
	}
	return data, name, nil
}

func (s *Server) uiProjectArtifactDownload(w http.ResponseWriter, r *http.Request) {
	if !projectArtifactDownloadPath(r.URL.Path) {
		writeError(w, http.StatusBadRequest, "invalid project artifact reference")
		return
	}
	project := r.PathValue("project")
	id := r.PathValue("artifact")
	if !consoleProjectID.MatchString(project) || !browserArtifactID.MatchString(id) {
		writeError(w, http.StatusBadRequest, "invalid project artifact reference")
		return
	}
	args, err := json.Marshal(map[string]string{"project_id": project, "artifact_id": id})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "invalid artifact lookup")
		return
	}
	result := s.invoke(r.Context(), "workspace__artifact_read_base64", args, json.RawMessage("1"))
	if failed, _ := result["isError"].(bool); failed {
		writeError(w, http.StatusBadGateway, "artifact unavailable")
		return
	}
	items, ok := result["content"].([]any)
	if !ok || len(items) != 1 {
		writeError(w, http.StatusBadGateway, "invalid artifact response")
		return
	}
	part, ok := items[0].(map[string]any)
	if !ok {
		writeError(w, http.StatusBadGateway, "invalid artifact response")
		return
	}
	raw, ok := part["text"].(string)
	if !ok {
		writeError(w, http.StatusBadGateway, "invalid artifact response")
		return
	}
	payload, filename, err := decodeBrowserArtifact([]byte(raw), id)
	if err != nil {
		writeError(w, http.StatusBadGateway, "artifact verification failed")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}
