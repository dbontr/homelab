package recipes

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestRecipeHistoryRestoreAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recipes.json")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	first, err := db.Save(sample("Version one"))
	if err != nil {
		t.Fatal(err)
	}
	second := first
	second.Title = "Version two"
	second, err = db.Save(second)
	if err != nil {
		t.Fatal(err)
	}
	third := second
	third.Ingredients[0].Amount = 9
	third, err = db.Save(third)
	if err != nil {
		t.Fatal(err)
	}
	history, err := db.History(third.ID)
	if err != nil || len(history) != 3 ||
		history[0].Revision != 3 || history[1].Revision != 2 || history[2].Revision != 1 {
		t.Fatalf("unexpected history: %+v %v", history, err)
	}
	history[2].Title = "Externally modified"
	history[0].Ingredients[0].Name = "Externally modified"
	reloaded, err := db.History(third.ID)
	if err != nil || reloaded[2].Title != "Version one" || reloaded[0].Ingredients[0].Name != "Tomato" {
		t.Fatal("history aliases external objects")
	}
	if _, err := db.Restore(third.ID, 1, 2); !errors.Is(err, ErrConflict) {
		t.Fatal("stale restore accepted")
	}
	restored, err := db.Restore(third.ID, 1, third.Revision)
	if err != nil || restored.Revision != 4 || restored.Title != "Version one" ||
		restored.Ingredients[0].Amount != 2 || !restored.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("restore failed: %+v %v", restored, err)
	}
	if _, err := db.Restore(third.ID, 2, third.Revision); !errors.Is(err, ErrConflict) {
		t.Fatal("repeat restore ignored the expected revision")
	}
	versions, err := db.History(third.ID)
	if err != nil || len(versions) != 4 || versions[0].Revision != 4 || versions[1].Revision != 3 {
		t.Fatalf("restore did not preserve audit trail: %+v %v", versions, err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := reopened.History(third.ID)
	if err != nil || len(persisted) != 4 || persisted[0].Title != "Version one" {
		t.Fatalf("history lost on restart: %+v %v", persisted, err)
	}
	archive := reopened.Export()
	destination, err := Open(filepath.Join(t.TempDir(), "import.json"))
	if err != nil {
		t.Fatal(err)
	}
	if count, err := destination.Import(archive); err != nil || count != 1 {
		t.Fatalf("archive import failed: %d %v", count, err)
	}
	imported, err := destination.History(third.ID)
	if err != nil || len(imported) != 4 || imported[2].Title != "Version two" {
		t.Fatalf("portable history was incomplete: %+v %v", imported, err)
	}
}

func TestRecipeHistoryRetentionAndInvalidArchive(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "recipes.json"))
	if err != nil {
		t.Fatal(err)
	}
	record, err := db.Save(sample("Iteration"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		record.Notes = "Updated note"
		record, err = db.Save(record)
		if err != nil {
			t.Fatal(err)
		}
	}
	history, err := db.History(record.ID)
	if err != nil || len(history) != MaxRecipeRevisions+1 ||
		history[0].Revision != 31 || history[len(history)-1].Revision != 11 {
		t.Fatalf("retention error: %+v %v", history, err)
	}
	if _, err := db.Restore(record.ID, 1, record.Revision); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired revision can be restored: %v", err)
	}
	if restored, err := db.Restore(record.ID, 11, record.Revision); err != nil || restored.Revision != 32 {
		t.Fatalf("retained revision restore failed: %+v %v", restored, err)
	}
	doc := db.Export()
	doc.History[record.ID][0].ID = "wrong"
	target, err := Open(filepath.Join(t.TempDir(), "import.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := target.Import(doc); err == nil || len(target.List("")) != 0 {
		t.Fatalf("invalid historical revision changed imported records: %v", err)
	}
}

func TestRecipeHistoryIsRemovedOnDeletion(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "recipes.json"))
	if err != nil {
		t.Fatal(err)
	}
	r, err := db.Save(sample("Delete history"))
	if err != nil {
		t.Fatal(err)
	}
	r.Title = "Another title"
	r, err = db.Save(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(r.ID, r.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := db.History(r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted recipe history still visible: %v", err)
	}
	if len(db.Export().History) != 0 {
		t.Fatal("deleted recipe left revision history in archive")
	}
}
