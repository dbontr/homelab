package store

/*
#include <stdlib.h>
#include <sqlite3.h>
*/
import "C"

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"
)

// Backup writes a transactionally consistent, self-contained SQLite snapshot.
// It does not export secrets or learner data through the MCP tool interface.
func (s *Store) Backup(destination string) (err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if destination == "" {
		return errors.New("backup destination must not be empty")
	}
	dest, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	source, err := filepath.Abs(s.path)
	if err != nil {
		return err
	}
	if dest == source {
		return errors.New("backup destination cannot be the source database")
	}
	if _, err := os.Lstat(dest); err == nil {
		return errors.New("backup destination already exists")
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		return err
	}
	tmp := dest + ".temporary-" + uuid()
	defer os.Remove(tmp)
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	db, err := openDatabase(tmp)
	if err != nil {
		return err
	}
	mainDB := C.CString("main")
	defer C.free(unsafe.Pointer(mainDB))
	backup := C.sqlite3_backup_init(db.handle, mainDB, s.db.handle, mainDB)
	if backup == nil {
		e := db.err()
		db.close()
		return fmt.Errorf("initialize SQLite backup: %w", e)
	}
	step := C.sqlite3_backup_step(backup, -1)
	finish := C.sqlite3_backup_finish(backup)
	if step != C.SQLITE_DONE || finish != C.SQLITE_OK {
		e := db.err()
		db.close()
		return fmt.Errorf("SQLite backup did not complete (step %d, finish %d): %w", int(step), int(finish), e)
	}
	if err = db.close(); err != nil {
		return err
	}
	if err = os.Chmod(tmp, 0600); err != nil {
		return err
	}
	if _, err = os.Lstat(dest); err == nil {
		return errors.New("backup destination already exists")
	} else if !os.IsNotExist(err) {
		return err
	}
	if err = os.Rename(tmp, dest); err != nil {
		return err
	}
	return nil
}
