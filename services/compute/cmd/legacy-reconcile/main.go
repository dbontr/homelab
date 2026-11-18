// Command legacy-reconcile audits historical project records against an independent restore.
// It does not modify either tree or infer project identities from repository names.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type Record struct {
	LegacyID    string   `json:"legacy_id"`
	Revision    string   `json:"revision"`
	ProjectID   string   `json:"project_id"`
	Kind        string   `json:"kind"`
	CreatedAt   string   `json:"created_at"`
	SourcePath  string   `json:"source_path"`
	RestorePath string   `json:"restore_path"`
	SHA256      string   `json:"sha256"`
	Size        int64    `json:"size"`
	Links       []string `json:"links,omitempty"`
}

type Manifest struct {
	Version int      `json:"version"`
	Records []Record `json:"records"`
}

type Finding struct {
	Code   string `json:"code"`
	Record string `json:"record,omitempty"`
	Path   string `json:"path,omitempty"`
}

type Report struct {
	Version         int       `json:"version"`
	ManifestSHA256  string    `json:"manifest_sha256"`
	Records         int       `json:"records"`
	SourceVerified  int       `json:"source_verified"`
	RestoreVerified int       `json:"restore_verified"`
	UnmappedFiles   int       `json:"unmapped_files"`
	UnreportedFiles int       `json:"unreported_files,omitempty"`
	Findings        []Finding `json:"findings"`
}

func recordKey(r Record) string { return r.LegacyID + "@" + r.Revision }

func cleanRelative(path string) (string, error) {
	if path == "" || strings.ContainsRune(path, '\\') || filepath.IsAbs(path) || strings.ContainsRune(path, '\x00') {
		return "", errors.New("invalid relative path")
	}
	cleaned := filepath.Clean(filepath.FromSlash(path))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) || filepath.ToSlash(cleaned) != path {
		return "", errors.New("unsafe or noncanonical path")
	}
	return cleaned, nil
}

// verifiedFile refuses symlinks at every component; two roots cannot share
// the same underlying inode and count as an independent recovery copy.
func verifiedFile(root, relative, digest string, size int64, other os.FileInfo) error {
	path, err := cleanRelative(relative)
	if err != nil {
		return err
	}
	current := root
	for _, component := range strings.Split(path, string(os.PathSeparator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("symlink in record path")
		}
	}
	info, err := os.Lstat(current)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != size {
		return errors.New("file type or size mismatch")
	}
	if other != nil && os.SameFile(info, other) {
		return errors.New("not an independent restored file")
	}
	file, err := os.Open(current)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != digest {
		return errors.New("SHA-256 mismatch")
	}
	return nil
}

func parseManifest(path string) (Manifest, string, error) {
	var m Manifest
	data, err := os.ReadFile(path)
	if err != nil {
		return m, "", err
	}
	digest := sha256.Sum256(data)
	if err := json.Unmarshal(data, &m); err != nil {
		return m, "", err
	}
	if m.Version != 1 {
		return m, "", errors.New("unsupported manifest version")
	}
	return m, hex.EncodeToString(digest[:]), nil
}

func validRecord(r Record) bool {
	if r.LegacyID == "" || r.ProjectID == "" || r.Kind == "" || r.Revision == "" || r.Size < 0 || len(r.SHA256) != 64 {
		return false
	}
	if _, err := time.Parse(time.RFC3339Nano, r.CreatedAt); err != nil {
		return false
	}
	if _, err := cleanRelative(r.SourcePath); err != nil {
		return false
	}
	if _, err := cleanRelative(r.RestorePath); err != nil {
		return false
	}
	decoded, err := hex.DecodeString(r.SHA256)
	return err == nil && len(decoded) == sha256.Size && hex.EncodeToString(decoded) == r.SHA256
}

func equalRecord(a, b Record) bool {
	if a.LegacyID != b.LegacyID || a.Revision != b.Revision || a.ProjectID != b.ProjectID ||
		a.Kind != b.Kind || a.CreatedAt != b.CreatedAt || a.SourcePath != b.SourcePath ||
		a.RestorePath != b.RestorePath || a.SHA256 != b.SHA256 || a.Size != b.Size {
		return false
	}
	return slices.Equal(a.Links, b.Links)
}

func audit(sourceRoot, restoreRoot string, original Manifest, restored *Manifest, manifestSHA string) Report {
	result := Report{Version: 1, ManifestSHA256: manifestSHA, Records: len(original.Records), Findings: []Finding{}}
	add := func(code, key, path string) { result.Findings = append(result.Findings, Finding{code, key, path}) }
	keys, sourcePaths, restorePaths := map[string]bool{}, map[string]bool{}, map[string]bool{}
	restoredByKey := map[string]Record{}
	if restored != nil {
		for _, r := range restored.Records {
			key := recordKey(r)
			if _, exists := restoredByKey[key]; exists {
				add("duplicate_restored_identity", key, r.RestorePath)
			}
			restoredByKey[key] = r
		}
	}
	for _, r := range original.Records {
		key := recordKey(r)
		if !validRecord(r) {
			add("invalid_record", key, r.SourcePath)
			continue
		}
		if keys[key] {
			add("duplicate_legacy_identity", key, r.SourcePath)
			continue
		}
		keys[key] = true
		if sourcePaths[r.SourcePath] {
			add("duplicate_source_path", key, r.SourcePath)
		}
		sourcePaths[r.SourcePath] = true
		if restorePaths[r.RestorePath] {
			add("duplicate_restore_path", key, r.RestorePath)
		}
		restorePaths[r.RestorePath] = true
		if err := verifiedFile(sourceRoot, r.SourcePath, r.SHA256, r.Size, nil); err != nil {
			add("source_unverified", key, r.SourcePath)
			continue
		}
		result.SourceVerified++
		if restored == nil || restoreRoot == "" {
			add("restore_not_audited", key, r.RestorePath)
			continue
		}
		target, exists := restoredByKey[key]
		if !exists {
			add("missing_restored_identity", key, r.RestorePath)
			continue
		}
		if !equalRecord(r, target) {
			add("restored_metadata_conflict", key, r.RestorePath)
			continue
		}
		originalInfo, err := os.Stat(filepath.Join(sourceRoot, filepath.FromSlash(r.SourcePath)))
		if err != nil {
			add("source_unverified", key, r.SourcePath)
			continue
		}
		if err := verifiedFile(restoreRoot, r.RestorePath, r.SHA256, r.Size, originalInfo); err != nil {
			add("restore_unverified", key, r.RestorePath)
			continue
		}
		result.RestoreVerified++
	}
	for _, r := range original.Records {
		if !keys[recordKey(r)] {
			continue
		}
		for _, link := range r.Links {
			if !keys[link] {
				add("unresolved_relationship", recordKey(r), link)
			}
		}
	}
	if restored != nil {
		for key, r := range restoredByKey {
			if !keys[key] {
				add("unexpected_restored_identity", key, r.RestorePath)
			}
		}
	}
	// A complete migration cannot be claimed while unclassified source files exist.
	_ = filepath.WalkDir(sourceRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			add("unreadable_source", "", "")
			return nil
		}
		if path == sourceRoot || entry.IsDir() {
			return nil
		}
		relative, relErr := filepath.Rel(sourceRoot, path)
		if relErr != nil {
			add("unreadable_source", "", "")
			return nil
		}
		relative = filepath.ToSlash(relative)
		if !sourcePaths[relative] {
			result.UnmappedFiles++
			if result.UnmappedFiles <= 100 {
				add("unmapped_legacy_file", "", relative)
			} else {
				result.UnreportedFiles++
			}
		}
		return nil
	})
	slices.SortFunc(result.Findings, func(a, b Finding) int {
		if c := strings.Compare(a.Code, b.Code); c != 0 {
			return c
		}
		if c := strings.Compare(a.Record, b.Record); c != 0 {
			return c
		}
		return strings.Compare(a.Path, b.Path)
	})
	return result
}

func run(args []string, stdout io.Writer) error {
	options := flag.NewFlagSet("legacy-reconcile", flag.ContinueOnError)
	options.SetOutput(io.Discard)
	source := options.String("source-root", "", "Read-only legacy source directory")
	restore := options.String("restore-root", "", "Independent restored directory")
	manifestPath := options.String("manifest", "", "Legacy record manifest")
	restoredManifest := options.String("restored-manifest", "", "Independently exported restored record manifest")
	if err := options.Parse(args); err != nil {
		return err
	}
	if options.NArg() != 0 || *source == "" || *manifestPath == "" {
		return errors.New("require --source-root and --manifest")
	}
	if (*restore == "") != (*restoredManifest == "") {
		return errors.New("restore root and manifest must be supplied together")
	}
	sourceInfo, err := os.Stat(*source)
	if err != nil || !sourceInfo.IsDir() {
		return errors.New("source root must be a directory")
	}
	if *restore != "" {
		restoreInfo, e := os.Stat(*restore)
		if e != nil || !restoreInfo.IsDir() || os.SameFile(sourceInfo, restoreInfo) {
			return errors.New("restore must be an independent directory")
		}
		a, _ := filepath.Abs(*source)
		b, _ := filepath.Abs(*restore)
		if strings.HasPrefix(a+string(os.PathSeparator), b+string(os.PathSeparator)) || strings.HasPrefix(b+string(os.PathSeparator), a+string(os.PathSeparator)) {
			return errors.New("source and restore directories overlap")
		}
	}
	original, hash, err := parseManifest(*manifestPath)
	if err != nil {
		return err
	}
	var independent *Manifest
	if *restoredManifest != "" {
		m, _, e := parseManifest(*restoredManifest)
		if e != nil {
			return e
		}
		independent = &m
	}
	result := audit(*source, *restore, original, independent, hash)
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		return err
	}
	if len(result.Findings) > 0 {
		return errors.New("reconciliation is incomplete")
	}
	return nil
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
