package recipes

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
)

const MaxImageBytes = 5 << 20

var (
	ErrImageTooLarge = errors.New("recipe image exceeds 5 MiB")
	ErrInvalidImage  = errors.New("recipe image must be a valid JPEG or PNG")
)

const imageURLPrefix = "/v1/recipes/images/"

func imageMediaType(name string) (string, bool) {
	if len(name) != 68 {
		return "", false
	}
	for _, digit := range name[:64] {
		if (digit < '0' || digit > '9') && (digit < 'a' || digit > 'f') {
			return "", false
		}
	}
	switch name[64:] {
	case ".jpg":
		return "image/jpeg", true
	case ".png":
		return "image/png", true
	default:
		return "", false
	}
}

func validImageRef(ref string) bool {
	if !strings.HasPrefix(ref, imageURLPrefix) {
		return false
	}
	_, ok := imageMediaType(strings.TrimPrefix(ref, imageURLPrefix))
	return ok
}

func (s *Store) imageDirectory() string {
	return filepath.Join(filepath.Dir(s.path), "images")
}

// PutImage writes an immutable, content-addressed image to the recipe store.
// Images are private to the same owner-session or bearer permissions as recipes.
func (s *Store) PutImage(content []byte, declaredType string) (string, error) {
	if len(content) > MaxImageBytes {
		return "", ErrImageTooLarge
	}
	if len(content) == 0 {
		return "", ErrInvalidImage
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil || config.Width < 1 || config.Height < 1 ||
		config.Width > 6000 || config.Height > 6000 ||
		int64(config.Width)*int64(config.Height) > 16_000_000 {
		return "", ErrInvalidImage
	}
	var extension string
	switch format {
	case "jpeg":
		if declaredType != "image/jpeg" {
			return "", ErrInvalidImage
		}
		extension = ".jpg"
	case "png":
		if declaredType != "image/png" {
			return "", ErrInvalidImage
		}
		extension = ".png"
	default:
		return "", ErrInvalidImage
	}

	sum := sha256.Sum256(content)
	name := hex.EncodeToString(sum[:]) + extension
	directory := s.imageDirectory()
	s.mu.Lock()
	defer s.mu.Unlock()
	// Decode only after bounding dimensions, and serialize the allocation for
	// concurrent uploads handled by this store.
	if _, decodedFormat, err := image.Decode(bytes.NewReader(content)); err != nil || decodedFormat != format {
		return "", ErrInvalidImage
	}

	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return "", errors.New("recipe image directory must be private")
	}
	target := filepath.Join(directory, name)
	if current, err := os.Lstat(target); err == nil {
		if !current.Mode().IsRegular() || current.Mode().Perm()&0077 != 0 ||
			current.Size() != int64(len(content)) {
			return "", errors.New("stored recipe image is invalid")
		}
		// Another upload of the same bytes reuses the immutable image.
		existing, err := os.ReadFile(target)
		if err != nil || !bytes.Equal(existing, content) {
			return "", errors.New("stored recipe image does not match its address")
		}
		return imageURLPrefix + name, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	file, err := os.CreateTemp(directory, ".image-")
	if err != nil {
		return "", err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := file.Chmod(0600); err != nil {
		return "", err
	}
	if _, err := file.Write(content); err != nil {
		return "", err
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	// A hard link publishes a complete file without replacing another asset.
	if err := os.Link(file.Name(), target); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return "", err
		}
		existing, readErr := os.ReadFile(target)
		if readErr != nil || !bytes.Equal(existing, content) {
			return "", errors.New("stored recipe image does not match its address")
		}
	}
	return imageURLPrefix + name, nil
}

// GetImage only reads immutable files whose names match their SHA-256 contents.
func (s *Store) GetImage(name string) ([]byte, string, error) {
	mediaType, ok := imageMediaType(name)
	if !ok {
		return nil, "", ErrNotFound
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	path := filepath.Join(s.imageDirectory(), name)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 ||
		info.Size() < 1 || info.Size() > MaxImageBytes {
		return nil, "", ErrInvalidImage
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(content)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), name[:64]) {
		return nil, "", ErrInvalidImage
	}
	return content, mediaType, nil
}
