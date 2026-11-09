package agentflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Archived records remain in the workflow owner's private state directory.
// They retain their original IDs and cannot be claimed or modified.
func (s *Store) archiveDirectory() (string, error) {
	dir := filepath.Join(s.dir, "archive")
	if err := os.Mkdir(dir, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return "", errors.New("workflow archive directory must be private")
	}
	return dir, nil
}

func (s *Store) archivePath(id string) (string, error) {
	if !validID(id) {
		return "", errors.New("invalid archived workflow ID")
	}
	dir, err := s.archiveDirectory()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, id+".json"), nil
}

func (s *Store) readArchive(id string) (Workflow, error) {
	location, err := s.archivePath(id)
	if err != nil {
		return Workflow{}, err
	}
	return s.readPath(location, id)
}

type archiveCandidate struct {
	id       string
	modified time.Time
}

func archiveSourceEntries(dir string) ([]archiveCandidate, error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	records := make([]archiveCandidate, 0, len(files))
	for _, file := range files {
		name := file.Name()
		if file.IsDir() || filepath.Ext(name) != ".json" {
			continue
		}
		id := name[:len(name)-5]
		if !validID(id) {
			continue
		}
		info, err := file.Info()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return nil, errors.New("unsafe workflow archive candidate")
		}
		records = append(records, archiveCandidate{id: id, modified: info.ModTime()})
	}
	sort.Slice(records, func(i, j int) bool {
		if !records[i].modified.Equal(records[j].modified) {
			return records[i].modified.Before(records[j].modified)
		}
		return records[i].id < records[j].id
	})
	return records, nil
}

// Only settled capability workflows may leave the active store. Engineering
// runs must remain available for their independent cleanup reconciliation.
func terminalCapabilityWorkflow(w Workflow, now time.Time) bool {
	if !w.CapabilityPlan {
		return false
	}
	for _, step := range w.Steps {
		if step.Status == Running {
			return false
		}
	}
	if w.Cancelled || w.PlanError != "" {
		return true
	}
	if !w.Deadline.IsZero() && !now.Before(w.Deadline) {
		return true
	}
	complete := len(w.Steps) > 0
	for _, step := range w.Steps {
		if step.Status == Failed {
			return true
		}
		if step.Status != Succeeded {
			complete = false
		}
	}
	return complete && (!w.AutoReplan || w.PlanComplete || w.Replans >= 2)
}

// ArchiveTerminal moves settled workflows under a cross-process lock.
// minAge is measured from the last durable record write, and a live lease
// always prevents archival. Directory syncs confirm the rename on both sides.
func (s *Store) ArchiveTerminal(minAge time.Duration, limit int) ([]string, error) {
	if minAge < time.Hour || limit < 1 || limit > 100 {
		return nil, errors.New("invalid archival limits")
	}
	unlock, err := s.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()

	archiveDir, err := s.archiveDirectory()
	if err != nil {
		return nil, err
	}
	candidates, err := archiveSourceEntries(s.dir)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	moved := make([]string, 0, limit)
	for _, candidate := range candidates {
		if len(moved) >= limit {
			break
		}
		if candidate.modified.After(now.Add(-minAge)) {
			continue
		}
		w, err := s.read(candidate.id)
		if err != nil {
			return moved, err
		}
		if !terminalCapabilityWorkflow(w, now) {
			continue
		}
		dest := filepath.Join(archiveDir, candidate.id+".json")
		if _, err := os.Lstat(dest); err == nil {
			return moved, fmt.Errorf("archived workflow already exists: %s", candidate.id)
		} else if !errors.Is(err, os.ErrNotExist) {
			return moved, err
		}
		if err := os.Rename(s.filename(candidate.id), dest); err != nil {
			return moved, err
		}
		moved = append(moved, candidate.id)
	}
	if len(moved) == 0 {
		return moved, nil
	}
	for _, dir := range []string{s.dir, archiveDir} {
		f, err := os.Open(dir)
		if err != nil {
			return moved, err
		}
		syncErr := f.Sync()
		closeErr := f.Close()
		if syncErr != nil {
			return moved, syncErr
		}
		if closeErr != nil {
			return moved, closeErr
		}
	}
	return moved, nil
}

// ListArchived returns only IDs; authenticated detail reads own access to
// recorded observations and evidence.
func (s *Store) ListArchived(limit int) ([]string, error) {
	if limit < 1 || limit > 100 {
		return nil, errors.New("invalid archive listing limit")
	}
	unlock, err := s.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()

	dir, err := s.archiveDirectory()
	if err != nil {
		return nil, err
	}
	entries, err := archiveSourceEntries(dir)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, limit)
	for i := len(entries) - 1; i >= 0 && len(ids) < limit; i-- {
		ids = append(ids, entries[i].id)
	}
	return ids, nil
}
