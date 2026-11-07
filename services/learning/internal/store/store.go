package store

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type Value = map[string]any

type Store struct {
	db               *database
	path             string
	mu               sync.Mutex
	trustedVerifiers map[string]ed25519.PublicKey
}

const minConfidence = .75

var slug = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
var dimensions = []string{"recognition", "application", "justification", "transfer", "independent_reconstruction", "retention"}

func nonempty(s, label string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("%s must not be empty", label)
	}
	if len(s) > 20000 {
		return "", fmt.Errorf("%s is too long", label)
	}
	return s, nil
}
func valid(s string, options ...string) bool {
	for _, x := range options {
		if s == x {
			return true
		}
	}
	return false
}
func uuid() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func now() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000000+00:00") }
func js(v any) string {
	b, e := json.Marshal(v)
	if e != nil {
		panic(e)
	}
	return string(b)
}
func parseList(v any) []string {
	out := []string{}
	if strVal(v) == "" {
		return out
	}
	_ = json.Unmarshal([]byte(strVal(v)), &out)
	return out
}
func stringsUnique(xs []string) bool {
	seen := map[string]bool{}
	for _, x := range xs {
		if seen[x] {
			return false
		}
		seen[x] = true
	}
	return true
}
func sortedSet(m map[string]bool) []string {
	x := []string{}
	for k := range m {
		x = append(x, k)
	}
	sort.Strings(x)
	return x
}
func set(xs []string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[x] = true
	}
	return m
}
func mapRows(rows []row) []Value {
	out := make([]Value, 0, len(rows))
	for _, r := range rows {
		out = append(out, Value(r))
	}
	return out
}
func getRows(db *database, q string, args ...any) []row {
	r, e := db.rows(q, args...)
	if e != nil {
		panic(e)
	}
	return r
}
func getRow(db *database, q string, args ...any) row {
	r, e := db.one(q, args...)
	if e != nil {
		panic(e)
	}
	return r
}
func exec(db *database, q string, args ...any) {
	if e := db.run(q, args...); e != nil {
		panic(e)
	}
}
func ensureRow(row row, kind, id string) row {
	if row == nil {
		panic(fmt.Errorf("unknown %s: %s", kind, id))
	}
	return row
}
func mustString(s, label string) string {
	v, e := nonempty(s, label)
	if e != nil {
		panic(e)
	}
	return v
}
func insist(ok bool, message string) {
	if !ok {
		panic(errors.New(message))
	}
}

// with serializes database transactions; a single learner's operations are small.
// SQLite WAL continues to support concurrent readers from external tools.
func (s *Store) with(fn func(*database) Value) (result Value, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer func() {
		if p := recover(); p != nil {
			if e, ok := p.(error); ok {
				err = e
			} else {
				err = fmt.Errorf("internal storage operation failed: %v", p)
			}
			result = nil
			_ = s.db.run("ROLLBACK")
		}
	}()
	if e := s.db.run("BEGIN IMMEDIATE"); e != nil {
		return nil, e
	}
	result = fn(s.db)
	if e := s.db.run("COMMIT"); e != nil {
		_ = s.db.run("ROLLBACK")
		return nil, e
	}
	return result, nil
}
func (s *Store) Close() error { s.mu.Lock(); defer s.mu.Unlock(); return s.db.close() }
func Open(path string) (*Store, error) {
	keys, err := verifierKeys(os.Getenv("ADAPTIVE_TEACHING_VERIFIER_KEYS"))
	if err != nil {
		return nil, fmt.Errorf("verifier configuration: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := openDatabase(path)
	if err != nil {
		return nil, err
	}
	if err := db.script(embeddedSchema); err != nil {
		db.close()
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		db.close()
		return nil, err
	}
	return &Store{db: db, path: path, trustedVerifiers: keys}, nil
}
