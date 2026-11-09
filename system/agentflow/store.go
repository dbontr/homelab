// Package agentflow coordinates durable, dependency-aware engineering jobs.
// It does not execute commands or possess deployment credentials.
package agentflow

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"
)

type Status string

const (
	Pending   Status = "pending"
	Running   Status = "running"
	Succeeded Status = "succeeded"
	Failed    Status = "failed"
	Cancelled Status = "cancelled"
)

type Step struct {
	ID          string          `json:"id"`
	DependsOn   []string        `json:"depends_on,omitempty"`
	Kind        string          `json:"kind"`
	Input       json.RawMessage `json:"input"`
	MaxAttempts int             `json:"max_attempts"`
	Status      Status          `json:"status"`
	Attempts    int             `json:"attempts"`
	Generation  uint64          `json:"generation"`
	LeaseDigest string          `json:"lease_digest,omitempty"`
	ExternalStarted bool `json:"external_started,omitempty"`
	LeaseUntil  time.Time       `json:"lease_until,omitempty"`
	LastError   string          `json:"last_error,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
}
type CleanupRecord struct {
	Status string `json:"status,omitempty"`
	Attempts int `json:"attempts,omitempty"`
	LastError string `json:"last_error,omitempty"`
}

type Workflow struct {
	CreatedAt time.Time `json:"created_at,omitempty"`
	Deadline time.Time `json:"deadline,omitempty"`
	Cleanup CleanupRecord `json:"cleanup,omitempty"`
	CleanupStarted time.Time `json:"cleanup_started,omitempty"`
	IntegrationTeamID string `json:"integration_team_id,omitempty"`
	TeamID string `json:"team_id,omitempty"`
	ID        string        `json:"id"`
	Project   string        `json:"project"`
	Revision  uint64        `json:"revision"`
	Cancelled bool          `json:"cancelled"`
	Steps     []Step        `json:"steps"`
	Agent     *AgentSession `json:"agent,omitempty"`
}
type Claim struct {
	WorkflowID string          `json:"workflow_id"`
	StepID     string          `json:"step_id"`
	Generation uint64          `json:"generation"`
	Token      string          `json:"token"`
	Kind       string          `json:"kind"`
	Input      json.RawMessage `json:"input"`
	Deadline   time.Time       `json:"deadline"`
}

var errNoReady = os.ErrNotExist

type Store struct {
	mu  sync.Mutex
	dir string
	now func() time.Time
}

func Open(dir string) (*Store, error) {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir || dir == "/" {
		return nil, errors.New("absolute private state directory required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() || fi.Mode().Perm()&0077 != 0 || fi.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("workflow store is not private")
	}
	return &Store{dir: dir, now: time.Now}, nil
}
func validID(s string) bool {
	if len(s) < 1 || len(s) > 80 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
func randomToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
func (s *Store) filename(id string) string { return filepath.Join(s.dir, id+".json") }

// lock serializes readers and writers across distinct processes sharing a store.
func (s *Store) lock() (func(), error) {
	s.mu.Lock()
	f, err := os.OpenFile(filepath.Join(s.dir, ".lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		f.Close()
		s.mu.Unlock()
		return nil, errors.New("unsafe workflow lock")
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		s.mu.Unlock()
		return nil, err
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close(); s.mu.Unlock() }, nil
}
func (s *Store) read(id string) (Workflow, error) {
	var w Workflow
	if !validID(id) {
		return w, errors.New("invalid workflow ID")
	}
	f, err := os.Open(s.filename(id))
	if err != nil {
		return w, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return w, err
	}
	if !fi.Mode().IsRegular() || fi.Mode().Perm()&0077 != 0 || fi.Size() > 1<<20 {
		return w, errors.New("unsafe workflow record")
	}
	if err := json.NewDecoder(f).Decode(&w); err != nil {
		return w, err
	}
	if w.ID != id {
		return w, errors.New("workflow record ID mismatch")
	}
	return w, nil
}
func (s *Store) save(w Workflow) error {
	raw, err := json.Marshal(w)
	if err != nil {
		return err
	}
	if len(raw) > 1<<20 {
		return errors.New("workflow record exceeds limit")
	}
	f, err := os.CreateTemp(s.dir, ".workflow-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(raw); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), s.filename(w.ID)); err != nil {
		return err
	}
	d, err := os.Open(s.dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func Validate(w Workflow) error {
	if !validID(w.ID) || !validID(w.Project) || len(w.Steps) == 0 || len(w.Steps) > 128 {
		return errors.New("invalid workflow identity, project or size")
	}
	found := map[string]Step{}
	for _, st := range w.Steps {
		if !validID(st.ID) || !validID(st.Kind) || st.MaxAttempts < 1 || st.MaxAttempts > 5 || len(st.Input) > 64<<10 || !json.Valid(st.Input) || len(st.DependsOn) > 32 {
			return errors.New("invalid step")
		}
		if _, ok := found[st.ID]; ok {
			return fmt.Errorf("duplicate step %s", st.ID)
		}
		found[st.ID] = st
	}
	state := map[string]uint8{}
	var visit func(string) error
	visit = func(id string) error {
		if state[id] == 1 {
			return errors.New("workflow dependency cycle")
		}
		if state[id] == 2 {
			return nil
		}
		st, ok := found[id]
		if !ok {
			return errors.New("unknown dependency")
		}
		state[id] = 1
		deps := map[string]bool{}
		for _, dep := range st.DependsOn {
			if deps[dep] {
				return errors.New("duplicate dependency")
			}
			deps[dep] = true
			if err := visit(dep); err != nil {
				return err
			}
		}
		state[id] = 2
		return nil
	}
	for id := range found {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) Create(w Workflow) error {
	if err := Validate(w); err != nil {
		return err
	}
	unlock, err := s.lock()
	if err != nil {
		return err
	}
	defer unlock()
	if _, err := os.Stat(s.filename(w.ID)); err == nil {
		return os.ErrExist
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	count := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".json" {
			continue
		}
		if validID(name[:len(name)-5]) {
			count++
		}
	}
	if count >= 1000 {
		return errors.New("engineering run retention capacity reached")
	}
	for i := range w.Steps {
		w.Steps[i].Status = Pending
		w.Steps[i].Attempts = 0
		w.Steps[i].Generation = 0
		w.Steps[i].LeaseDigest = ""
		w.Steps[i].LeaseUntil = time.Time{}
		w.Steps[i].Result = nil
		w.Steps[i].LastError = ""
	}
	w.Revision = 1
	w.Cancelled = false
	if w.CreatedAt.IsZero(){w.CreatedAt=s.now().UTC()}
	return s.save(w)
}
func (s *Store) Get(id string) (Workflow, error) {
	unlock, err := s.lock()
	if err != nil {
		return Workflow{}, err
	}
	defer unlock()
	return s.read(id)
}

// ClaimReady reserves one eligible step. A lease expires without implying
// that its external side effect was rolled back. Callers must use a stable
// idempotency key (workflow ID, step ID) when dispatching to a worker.
func (s *Store) ClaimReady(id string, ttl time.Duration) (Claim, error) {
	var claim Claim
	if ttl < time.Second || ttl > 30*time.Minute {
		return claim, errors.New("invalid lease duration")
	}
	unlock, err := s.lock()
	if err != nil {
		return claim, err
	}
	defer unlock()
	w, err := s.read(id)
	if err != nil {
		return claim, err
	}
	if w.TeamID!="" {
		recordFile,err:=os.Open(s.teamPath(w.TeamID))
		if err!=nil{return claim,errNoReady}
		var record TeamRecord
		decodeErr:=json.NewDecoder(recordFile).Decode(&record)
		closeErr:=recordFile.Close()
		authorized:=false
		for _,member:=range record.Members{
			if member.RunID==w.ID {authorized=true;break}
		}
		if decodeErr!=nil||closeErr!=nil||record.ID!=w.TeamID||record.Project!=w.Project||!authorized{
			return claim,errNoReady
		}
	}
	if w.IntegrationTeamID!="" {
		recordFile,err:=os.Open(s.teamPath(w.IntegrationTeamID))
		if err!=nil{return claim,errNoReady}
		var record TeamRecord
		decodeErr:=json.NewDecoder(recordFile).Decode(&record)
		closeErr:=recordFile.Close()
		if decodeErr!=nil||closeErr!=nil||record.ID!=w.IntegrationTeamID||record.IntegrationRunID!=w.ID||record.Project!=w.Project {
			return claim,errNoReady
		}
	}
	if w.Cancelled {
		return claim, errors.New("workflow cancelled")
	}
	statuses := map[string]Status{}
	for _, st := range w.Steps {
		statuses[st.ID] = st.Status
	}
	for i := range w.Steps {
		st := &w.Steps[i]
		if st.Status == Running && s.now().Before(st.LeaseUntil) {
			continue
		}
		if st.Status != Pending && st.Status != Running {
			continue
		}
		if st.Attempts >= st.MaxAttempts {
			if st.Status == Running {
				st.Status = Failed
				st.LastError = "lease expired and attempts exhausted"
				w.Revision++
				if err := s.save(w); err != nil {
					return claim, err
				}
			}
			continue
		}
		ready := true
		for _, dep := range st.DependsOn {
			if statuses[dep] != Succeeded {
				ready = false
				break
			}
		}
		if !ready {
			continue
		}
		token, err := randomToken()
		if err != nil {
			return claim, err
		}
		st.Status = Running
		st.Attempts++
		st.Generation++
		st.LeaseUntil = s.now().Add(ttl)
		st.LastError = ""
		digest := sha256.Sum256([]byte(token))
		st.LeaseDigest = hex.EncodeToString(digest[:])
		w.Revision++
		if err := s.save(w); err != nil {
			return Claim{}, err
		}
		return Claim{WorkflowID: w.ID, StepID: st.ID, Generation: st.Generation, Token: token, Kind: st.Kind, Input: append(json.RawMessage(nil), st.Input...), Deadline: st.LeaseUntil}, nil
	}
	return claim, errNoReady
}

// Complete requires both the active generation and the corresponding secret
// lease token. Only the token digest is persisted.
// The executor must enforce its own authorization and idempotency contract.
func (s *Store) Complete(c Claim, result json.RawMessage, executionError string) error {
	if !validID(c.WorkflowID) || !validID(c.StepID) || c.Generation == 0 || len(c.Token) != 64 {
		return errors.New("invalid claim")
	}
	if len(result) > 64<<10 || (len(result) > 0 && !json.Valid(result)) || len(executionError) > 1024 {
		return errors.New("invalid result")
	}
	unlock, err := s.lock()
	if err != nil {
		return err
	}
	defer unlock()
	w, err := s.read(c.WorkflowID)
	if err != nil {
		return err
	}
	if w.Cancelled {
		return errors.New("workflow cancelled")
	}
	for i := range w.Steps {
		st := &w.Steps[i]
		if st.ID != c.StepID {
			continue
		}
		if st.Status != Running || st.Generation != c.Generation || !s.now().Before(st.LeaseUntil) {
			return errors.New("claim stale or expired")
		}
		digest := sha256.Sum256([]byte(c.Token))
		expected, err := hex.DecodeString(st.LeaseDigest)
		if err != nil || subtle.ConstantTimeCompare(expected, digest[:]) != 1 {
			return errors.New("invalid claim token")
		}
		// The lease token and generation jointly fence concurrent workers and restarts.
		if executionError != "" {
			st.LastError = executionError
			if st.Attempts >= st.MaxAttempts {
				st.Status = Failed
			} else {
				st.Status = Pending
			}
		} else {
			st.Status = Succeeded
			st.Result = append(json.RawMessage(nil), result...)
			st.LastError = ""
		}
		st.LeaseUntil = time.Time{}
		st.LeaseDigest = ""
		w.Revision++
		return s.save(w)
	}
	return os.ErrNotExist
}
func (s *Store) Cancel(id string, expectedRevision uint64) error {
	unlock, err := s.lock()
	if err != nil {
		return err
	}
	defer unlock()
	w, err := s.read(id)
	if err != nil {
		return err
	}
	if w.Revision != expectedRevision {
		return errors.New("workflow revision conflict")
	}
	if w.Cancelled {
		return nil
	}
	allSucceeded := true
	for _, step := range w.Steps {
		if step.Status == Failed {
			return errors.New("failed workflow cannot be cancelled")
		}
		if step.Status != Succeeded {
			allSucceeded = false
		}
	}
	if allSucceeded {
		return errors.New("completed workflow cannot be cancelled")
	}
	w.Cancelled = true
	for i := range w.Steps {
		if w.Steps[i].Status == Pending || w.Steps[i].Status == Running {
			w.Steps[i].Status = Cancelled
			w.Steps[i].LeaseUntil = time.Time{}
			w.Steps[i].LeaseDigest = ""
		}
	}
	w.Revision++
	return s.save(w)
}
func (s *Store) List() ([]string, error) {
	unlock, err := s.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".json" {
			continue
		}
		id := name[:len(name)-5]
		if validID(id) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids, nil
}
