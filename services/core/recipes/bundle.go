package recipes

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

const (
	MaxPortableBundleBytes = 96 << 20
	maxPortableManifestBytes = 16 << 20
	maxPortableImages = 128
)

var ErrBundleTooLarge = errors.New("recipe bundle exceeds supported size")

func recipeImageNames(doc Document) (map[string]bool, error) {
	names := map[string]bool{}
	collect := func(recipe Recipe) error {
		if recipe.ImageRef == "" {
			return nil
		}
		if !validImageRef(recipe.ImageRef) {
			return errors.New("invalid image reference in recipe archive")
		}
		names[strings.TrimPrefix(recipe.ImageRef, imageURLPrefix)] = true
		return nil
	}
	for _, recipe := range doc.Recipes {
		if err := collect(recipe); err != nil { return nil, err }
	}
	for _, versions := range doc.History {
		for _, recipe := range versions {
			if err := collect(recipe); err != nil { return nil, err }
		}
	}
	if len(names) > maxPortableImages {
		return nil, ErrBundleTooLarge
	}
	return names, nil
}

func readZipFile(entry *zip.File, limit int) ([]byte, error) {
	if !entry.FileInfo().Mode().IsRegular() || entry.UncompressedSize64 > uint64(limit) {
		return nil, ErrBundleTooLarge
	}
	reader, err := entry.Open()
	if err != nil { return nil, err }
	defer reader.Close()
	body, err := io.ReadAll(io.LimitReader(reader, int64(limit)+1))
	if err != nil { return nil, err }
	if len(body) > limit { return nil, ErrBundleTooLarge }
	return body, nil
}

// ExportBundle is a portable ZIP containing the unchanged version-1
// JSON archive and the privately stored images it references, including
// images referenced by retained recipe history.
func (s *Store) ExportBundle() ([]byte, error) {
	doc := s.Export()
	images, err := recipeImageNames(doc)
	if err != nil { return nil, err }
	manifest, err := json.MarshalIndent(doc, "", "  ")
	if err != nil { return nil, err }
	if len(manifest) > maxPortableManifestBytes { return nil, ErrBundleTooLarge }

	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	write := func(name string, body []byte) error {
		entry, err := writer.Create(name)
		if err != nil { return err }
		_, err = entry.Write(body)
		return err
	}
	if err := write("recipes.json", manifest); err != nil { writer.Close(); return nil, err }
	names := make([]string, 0, len(images))
	for name := range images { names = append(names, name) }
	sort.Strings(names)
	total := len(manifest)
	for _, name := range names {
		body, _, err := s.GetImage(name)
		if err != nil {
			writer.Close()
			return nil, fmt.Errorf("recipe archive image %s: %w", name, err)
		}
		total += len(body)
		if total > MaxPortableBundleBytes {
			writer.Close()
			return nil, ErrBundleTooLarge
		}
		if err := write("images/"+name, body); err != nil {
			writer.Close()
			return nil, err
		}
	}
	if err := writer.Close(); err != nil { return nil, err }
	if buffer.Len() > MaxPortableBundleBytes { return nil, ErrBundleTooLarge }
	return buffer.Bytes(), nil
}

// ImportBundle rejects unreferenced files, missing images, duplicate paths,
// bad image digests and oversized entries before writing any image data.
// Recipe records use the existing non-overwriting, atomic JSON importer.
func (s *Store) ImportBundle(content []byte) (int, error) {
	if len(content) == 0 || len(content) > MaxPortableBundleBytes {
		return 0, ErrBundleTooLarge
	}
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil { return 0, errors.New("invalid recipe ZIP archive") }
	if len(reader.File) == 0 || len(reader.File) > maxPortableImages+1 {
		return 0, errors.New("unexpected number of recipe archive entries")
	}

	var manifest []byte
	files := map[string][]byte{}
	seen := map[string]bool{}
	// The compressed ZIP limit alone does not constrain decompressed memory.
	// Apply the same aggregate ceiling while reading entries.
	remaining := MaxPortableBundleBytes
	for _, file := range reader.File {
		if seen[file.Name] { return 0, errors.New("duplicate recipe archive entry") }
		seen[file.Name] = true
		switch {
		case file.Name == "recipes.json":
			if file.UncompressedSize64 > uint64(remaining) { return 0, ErrBundleTooLarge }
			manifest, err = readZipFile(file, maxPortableManifestBytes)
			if err != nil { return 0, err }
			remaining -= len(manifest)
		case strings.HasPrefix(file.Name, "images/"):
			name := strings.TrimPrefix(file.Name, "images/")
			if _, ok := imageMediaType(name); !ok {
				return 0, errors.New("invalid image name in recipe archive")
			}
			if file.UncompressedSize64 > uint64(remaining) { return 0, ErrBundleTooLarge }
			body, readErr := readZipFile(file, MaxImageBytes)
			if readErr != nil { return 0, readErr }
			if len(body) > remaining { return 0, ErrBundleTooLarge }
			remaining -= len(body)
			digest := sha256.Sum256(body)
			if hex.EncodeToString(digest[:]) != name[:64] {
				return 0, errors.New("recipe archive image digest mismatch")
			}
			files[name] = body
		default:
			return 0, errors.New("unexpected file in recipe archive")
		}
	}
	if len(manifest) == 0 { return 0, errors.New("recipe archive manifest is missing") }
	var doc Document
	if err := json.Unmarshal(manifest, &doc); err != nil {
		return 0, errors.New("invalid recipe archive manifest")
	}
	images, err := recipeImageNames(doc)
	if err != nil { return 0, err }
	if len(images) != len(files) {
		return 0, errors.New("recipe archive images do not match their references")
	}
	for name := range files {
		if !images[name] {
			return 0, errors.New("recipe archive contains an unreferenced image")
		}
	}
	// PutImage fully decodes each JPEG/PNG, checks its content hash, and
	// publishes immutable files. Failed record imports may leave unused
	// immutable images, which are safe to reuse in a later import.
	for name, body := range files {
		mediaType, _ := imageMediaType(name)
		ref, err := s.PutImage(body, mediaType)
		if err != nil { return 0, err }
		if ref != imageURLPrefix+name {
			return 0, errors.New("recipe archive image identity mismatch")
		}
	}
	return s.Import(doc)
}
