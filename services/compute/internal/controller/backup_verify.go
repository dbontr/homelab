package controller

import (
	"errors"
	"fmt"
	"path/filepath"
)

// verifyRestoredState validates the complete recovered dataset before any
// database or artifacts are promoted into the requested destination.
func verifyRestoredState(directory string, manifestFiles map[string]bool) error {
	db, err := OpenDatabase(filepath.Join(directory, "state.db"))
	if err != nil {
		return fmt.Errorf("restored SQLite database cannot be opened: %w", err)
	}
	defer db.Close()

	integrity, err := db.query("PRAGMA integrity_check")
	if err != nil || len(integrity) != 1 || asString(integrity[0]["integrity_check"]) != "ok" {
		return errors.New("restored SQLite database failed integrity check")
	}
	foreignKeys, err := db.query("PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("restored foreign-key check failed: %w", err)
	}
	if len(foreignKeys) != 0 {
		return errors.New("restored SQLite database has invalid foreign-key relationships")
	}

	artifacts, err := db.query("SELECT id,sha256,size FROM artifacts")
	if err != nil {
		return err
	}
	for _, row := range artifacts {
		id, digest := asString(row["id"]), asString(row["sha256"])
		relative := filepath.Join("artifacts", id)
		if !backupPathAllowed(relative) || !manifestFiles[relative] {
			return fmt.Errorf("restored artifact %s is missing from the backup manifest", id)
		}
		hash, size, err := hashFile(filepath.Join(directory, relative))
		if err != nil || hash != digest || size != int64(asFloat(row["size"])) {
			return fmt.Errorf("restored artifact %s does not match SQLite metadata", id)
		}
	}
	objects, err := db.query("SELECT DISTINCT sha256,size FROM library_objects")
	if err != nil {
		return err
	}
	for _, row := range objects {
		digest := asString(row["sha256"])
		if !shaPattern.MatchString(digest) {
			return errors.New("restored library object has an invalid SHA-256")
		}
		relative := filepath.Join("artifacts", "objects", digest[:2], digest)
		if !manifestFiles[relative] {
			return fmt.Errorf("restored library object %s is missing from the backup manifest", digest)
		}
		hash, size, err := hashFile(filepath.Join(directory, relative))
		if err != nil || hash != digest || size != int64(asFloat(row["size"])) {
			return fmt.Errorf("restored library object %s does not match SQLite metadata", digest)
		}
	}
	// The Kueue receipt and durable result must identify the same project.
	// SQLite foreign keys enforce existence, but not this cross-table equality.
	inconsistent, err := db.query(`SELECT k.job FROM kueue_jobs k JOIN jobs j ON j.id=k.job
		WHERE k.project<>j.project LIMIT 1`)
	if err != nil {
		return err
	}
	if len(inconsistent) != 0 {
		return errors.New("restored Kueue job has mismatched project ownership")
	}
	contributions, err := db.query("SELECT id,body,body_sha256 FROM project_contributions")
	if err != nil {
		return err
	}
	for _, row := range contributions {
		if checksum([]byte(asString(row["body"]))) != asString(row["body_sha256"]) {
			return fmt.Errorf("restored contribution %s has an invalid content digest", asString(row["id"]))
		}
	}
	return nil
}
