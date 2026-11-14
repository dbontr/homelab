package recipes

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func smallImage(t *testing.T, format string) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	img.Set(0, 0, color.RGBA{R: 250, G: 30, B: 25, A: 255})
	var buffer bytes.Buffer
	var err error
	if format == "png" {
		err = png.Encode(&buffer, img)
	} else {
		err = jpeg.Encode(&buffer, img, &jpeg.Options{Quality: 85})
	}
	if err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestRecipeImagePersistenceAndDeduplication(t *testing.T) {
	path := privateRecipeTestPath(t, "recipes.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		format, contentType, suffix string
	}{
		{"png", "image/png", ".png"},
		{"jpeg", "image/jpeg", ".jpg"},
	} {
		content := smallImage(t, test.format)
		ref, err := store.PutImage(content, test.contentType)
		if err != nil || !strings.HasPrefix(ref, imageURLPrefix) || !strings.HasSuffix(ref, test.suffix) {
			t.Fatalf("image upload: ref %q, error %v", ref, err)
		}
		second, err := store.PutImage(content, test.contentType)
		if err != nil || second != ref {
			t.Fatalf("content-addressed duplicate: %s %v", second, err)
		}
		info, err := os.Stat(filepath.Join(store.imageDirectory(), strings.TrimPrefix(ref, imageURLPrefix)))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("private image file: %v %v", info, err)
		}
		reopened, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		body, mediaType, err := reopened.GetImage(strings.TrimPrefix(ref, imageURLPrefix))
		if err != nil || mediaType != test.contentType || !bytes.Equal(body, content) {
			t.Fatalf("image retrieval across restarts: %s %v", mediaType, err)
		}
		recipe := sample("Photo recipe")
		recipe.ImageRef = ref
		if _, err := store.Save(recipe); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRecipeImageInputValidationAndTampering(t *testing.T) {
	store, err := Open(privateRecipeTestPath(t, "recipes.json"))
	if err != nil {
		t.Fatal(err)
	}
	pngImage := smallImage(t, "png")
	for _, test := range []struct {
		data []byte
		kind string
		want error
	}{
		{[]byte("<svg><script>bad</script></svg>"), "image/png", ErrInvalidImage},
		{pngImage, "image/jpeg", ErrInvalidImage},
		{pngImage[:len(pngImage)-8], "image/png", ErrInvalidImage},
		{pngImage, "text/html", ErrInvalidImage},
		{nil, "image/png", ErrInvalidImage},
		{bytes.Repeat([]byte{1}, MaxImageBytes + 1), "image/png", ErrImageTooLarge},
	} {
		if _, err := store.PutImage(test.data, test.kind); !errors.Is(err, test.want) {
			t.Fatalf("invalid upload accepted: %v", err)
		}
	}
	for _, name := range []string{"../recipes.json", "invalid.png", strings.Repeat("a", 64) + ".gif", strings.Repeat("A", 64) + ".png"} {
		if _, _, err := store.GetImage(name); !errors.Is(err, ErrNotFound) {
			t.Fatalf("unsafe image read %q: %v", name, err)
		}
		recipe := sample("Bad reference")
		recipe.ImageRef = imageURLPrefix + name
		if _, err := store.Save(recipe); err == nil {
			t.Fatalf("unsafe image ref accepted: %q", name)
		}
	}
	ref, err := store.PutImage(pngImage, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.imageDirectory(), strings.TrimPrefix(ref, imageURLPrefix)), bytes.Repeat([]byte{0}, len(pngImage)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.GetImage(strings.TrimPrefix(ref, imageURLPrefix)); !errors.Is(err, ErrInvalidImage) {
		t.Fatalf("corrupt image returned: %v", err)
	}
}

func TestRecipeImageRejectsUnsafeStorageDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "recipes")
	store, err := Open(filepath.Join(root, "recipes.json"))
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "external")
	if err := os.Mkdir(destination, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(destination, store.imageDirectory()); err != nil {
		t.Skipf("symbolic links are not available: %v", err)
	}
	if _, err := store.PutImage(smallImage(t, "png"), "image/png"); err == nil {
		t.Fatal("symbolic-link image directory accepted")
	}
}
