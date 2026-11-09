package recipes

import (
	"errors"
	"fmt"
	"time"
)

// MaxRecipeRevisions is the number of previous versions retained per recipe.
const MaxRecipeRevisions = 20

func copyHistory(source []Recipe) []Recipe {
	out := make([]Recipe, len(source))
	for i, recipe := range source {
		out[i] = copyRecipe(recipe)
	}
	return out
}

func appendRevision(versions []Recipe, previous Recipe) []Recipe {
	versions = append(copyHistory(versions), copyRecipe(previous))
	if len(versions) > MaxRecipeRevisions {
		versions = versions[len(versions)-MaxRecipeRevisions:]
	}
	return versions
}

func validateHistory(id string, current Recipe, versions []Recipe) error {
	if len(versions) > MaxRecipeRevisions {
		return errors.New("recipe revision history exceeds retention limit")
	}
	last := 0
	for _, version := range versions {
		if version.ID != id || version.Revision <= last ||
			version.Revision >= current.Revision ||
			version.CreatedAt.IsZero() || version.UpdatedAt.IsZero() ||
			!version.CreatedAt.Equal(current.CreatedAt) {
			return errors.New("recipe history has inconsistent identity or revisions")
		}
		if err := validateRecipe(version); err != nil {
			return fmt.Errorf("invalid historical recipe revision: %w", err)
		}
		last = version.Revision
	}
	if len(versions) > 0 && last != current.Revision-1 {
		return errors.New("recipe history does not end at the prior revision")
	}
	return nil
}

// History returns the current recipe first, followed by available prior versions.
// The oldest versions are discarded after the retention limit is reached.
func (s *Store) History(id string) ([]Recipe, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	current, ok := s.data.Recipes[id]
	if !ok {
		return nil, ErrNotFound
	}
	previous := s.data.History[id]
	out := make([]Recipe, 0, len(previous)+1)
	out = append(out, copyRecipe(current))
	for i := len(previous)-1; i >= 0; i-- {
		out = append(out, copyRecipe(previous[i]))
	}
	return out, nil
}

// Restore creates a new revision from an old snapshot, preserving the full
// audit trail and rejecting requests based on a stale current revision.
func (s *Store) Restore(id string, sourceRevision, expectedRevision int) (Recipe, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.data.Recipes[id]
	if !ok {
		return Recipe{}, ErrNotFound
	}
	if expectedRevision != current.Revision {
		return Recipe{}, ErrConflict
	}
	if sourceRevision < 1 || sourceRevision >= current.Revision {
		return Recipe{}, errors.New("restore requires an earlier recipe revision")
	}
	var restored Recipe
	found := false
	for _, version := range s.data.History[id] {
		if version.Revision == sourceRevision {
			restored = copyRecipe(version)
			found = true
			break
		}
	}
	if !found {
		return Recipe{}, ErrNotFound
	}
	restored.Revision = current.Revision + 1
	restored.CreatedAt = current.CreatedAt
	restored.UpdatedAt = time.Now().UTC()
	next := duplicate(s.data)
	next.History[id] = appendRevision(next.History[id], current)
	next.Recipes[id] = copyRecipe(restored)
	if err := s.save(next); err != nil {
		return Recipe{}, err
	}
	return copyRecipe(restored), nil
}
