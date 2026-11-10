// Package stateowner guards the files owned by one application process.
// Ownership is advisory and applies to participants that acquire the lease.
package stateowner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
)

// Lease holds operating-system locks for a collection of persistent resources.
// The kernel also releases the locks if the process exits without calling Close.
type Lease struct {
	mu    sync.Mutex
	files []*os.File
}

// Acquire reserves all resource paths without waiting for another writer.
// The paths must refer to durable files or directories, not to their lock files.
// A failed acquisition releases every lock acquired during that attempt.
func Acquire(paths ...string) (_ *Lease, err error) {
	if len(paths) == 0 {
		return nil, errors.New("at least one state resource is required")
	}
	unique := make(map[string]struct{}, len(paths))
	for _, resource := range paths {
		if resource == "" {
			return nil, errors.New("state resource path is empty")
		}
		absolute, err := filepath.Abs(resource)
		if err != nil {
			return nil, fmt.Errorf("resolve state resource: %w", err)
		}
		parent := filepath.Dir(absolute)
		if err := os.MkdirAll(parent, 0700); err != nil {
			return nil, fmt.Errorf("prepare state directory: %w", err)
		}
		canonical, err := filepath.EvalSymlinks(parent)
		if err != nil {
			return nil, fmt.Errorf("resolve state directory: %w", err)
		}
		resourcePath := filepath.Join(canonical, filepath.Base(absolute))
		if info, err := os.Lstat(resourcePath); err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("state resource %q must not be a symbolic link", resourcePath)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("inspect state resource: %w", err)
		}
		unique[resourcePath+".lock"] = struct{}{}
	}
	locks := make([]string, 0, len(unique))
	for path := range unique {
		locks = append(locks, path)
	}
	sort.Strings(locks)
	lease := &Lease{}
	defer func() {
		if err != nil {
			_ = lease.Close()
		}
	}()
	for _, lockPath := range locks {
		file, openErr := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
		if openErr != nil {
			return nil, fmt.Errorf("open protected state lock %q: %w", lockPath, openErr)
		}
		info, statErr := file.Stat()
		if statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			_ = file.Close()
			return nil, fmt.Errorf("state lock %q has unsafe permissions or type", lockPath)
		}
		if lockErr := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); lockErr != nil {
			_ = file.Close()
			return nil, fmt.Errorf("state resource %q is already owned or cannot be locked: %w", lockPath, lockErr)
		}
		lease.files = append(lease.files, file)
	}
	return lease, nil
}

// Close releases ownership; a later call has no effect.
func (l *Lease) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var result error
	for i := len(l.files) - 1; i >= 0; i-- {
		file := l.files[i]
		if err := syscall.Flock(int(file.Fd()), syscall.LOCK_UN); err != nil {
			result = errors.Join(result, err)
		}
		if err := file.Close(); err != nil {
			result = errors.Join(result, err)
		}
	}
	l.files = nil
	return result
}
