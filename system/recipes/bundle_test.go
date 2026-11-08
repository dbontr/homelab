package recipes

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"path/filepath"
	"testing"
)

func TestRecipeBundlePreservesImagesAndHistory(t *testing.T) {
	source, err := Open(filepath.Join(t.TempDir(), "recipes.json"))
	if err != nil { t.Fatal(err) }
	imageBytes := smallImage(t, "png")
	ref, err := source.PutImage(imageBytes, "image/png")
	if err != nil { t.Fatal(err) }
	recipe := sample("Cooked rice")
	recipe.ImageRef = ref
	recipe, err = source.Save(recipe)
	if err != nil { t.Fatal(err) }
	recipe.Title = "Rice with oil"
	recipe.ImageRef = ""
	recipe, err = source.Save(recipe)
	if err != nil { t.Fatal(err) }
	if _, err := source.AddMeal(Meal{
		RecipeID: recipe.ID, Date: "2026-11-19", Servings: 2,
	}); err != nil { t.Fatal(err) }

	bundle, err := source.ExportBundle()
	if err != nil { t.Fatal(err) }
	if len(bundle) == 0 || len(bundle) > MaxPortableBundleBytes {
		t.Fatal("invalid recipe ZIP size")
	}
	zipFile, err := zip.NewReader(bytes.NewReader(bundle), int64(len(bundle)))
	if err != nil || len(zipFile.File) != 2 { t.Fatalf("unexpected bundle content: %v %v", zipFile, err) }
	if zipFile.File[0].Name != "recipes.json" {
		t.Fatal("portable document manifest is not first")
	}
	dest, err := Open(filepath.Join(t.TempDir(), "recipes.json"))
	if err != nil { t.Fatal(err) }
	if count, err := dest.ImportBundle(bundle); err != nil || count != 1 {
		t.Fatalf("bundle import: %d %v", count, err)
	}
	got, err := dest.Get(recipe.ID)
	if err != nil || got.Title != recipe.Title || got.ImageRef != "" {
		t.Fatalf("wrong imported record: %+v %v", got, err)
	}
	versions, err := dest.History(recipe.ID)
	if err != nil || len(versions) != 2 || versions[1].ImageRef != ref {
		t.Fatalf("history lost image reference: %+v %v", versions, err)
	}
	name := strings.TrimPrefix(ref, imageURLPrefix)
	restored, kind, err := dest.GetImage(name)
	if err != nil || kind != "image/png" || !bytes.Equal(restored, imageBytes) {
		t.Fatalf("history image missing from portable archive: %v", err)
	}
	if len(dest.Meals()) != 1 { t.Fatal("planned meal missing from portable archive") }
	if _, err := dest.ImportBundle(bundle); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate bundle replaced existing recipe: %v", err)
	}
}

func writeTestBundle(t *testing.T, records map[string][]byte) []byte {
	t.Helper()
	var out bytes.Buffer
	writer := zip.NewWriter(&out)
	for name, body := range records {
		entry, err := writer.Create(name)
		if err != nil { t.Fatal(err) }
		if _, err := entry.Write(body); err != nil { t.Fatal(err) }
	}
	if err := writer.Close(); err != nil { t.Fatal(err) }
	return out.Bytes()
}

func TestRecipeBundleRejectsMissingAndUnexpectedAssets(t *testing.T) {
	source, err := Open(filepath.Join(t.TempDir(), "recipes.json"))
	if err != nil { t.Fatal(err) }
	body := smallImage(t, "png")
	ref, err := source.PutImage(body, "image/png")
	if err != nil { t.Fatal(err) }
	recipe := sample("Soup")
	recipe.ImageRef = ref
	_, err = source.Save(recipe)
	if err != nil { t.Fatal(err) }
	manifest, err := json.Marshal(source.Export())
	if err != nil { t.Fatal(err) }
	name := strings.TrimPrefix(ref, imageURLPrefix)
	dest, err := Open(filepath.Join(t.TempDir(), "recipes.json"))
	if err != nil { t.Fatal(err) }
	for _, files := range []map[string][]byte{
		{"recipes.json": manifest},
		{"recipes.json": manifest, "images/../../../credentials": body},
		{"recipes.json": manifest, "images/"+name: []byte("invalid picture")},
		{"recipes.json": manifest, "images/"+name: body, "unexpected.txt": []byte("text")},
	} {
		if _, err := dest.ImportBundle(writeTestBundle(t, files)); err == nil {
			t.Fatal("invalid recipe ZIP was accepted")
		}
		if len(dest.List("")) != 0 {
			t.Fatal("invalid recipe ZIP changed stored records")
		}
	}
	if _, err := dest.ImportBundle([]byte("not a zip")); err == nil {
		t.Fatal("invalid ZIP file was accepted")
	}
	if _, err := dest.ImportBundle(make([]byte, MaxPortableBundleBytes+1)); !errors.Is(err, ErrBundleTooLarge) {
		t.Fatalf("oversized archive accepted: %v", err)
	}
}
