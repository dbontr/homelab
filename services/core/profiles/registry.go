package profiles

import (
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

var identifier = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
var fieldName = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

type Requirements struct {
	Architecture string         `json:"architecture,omitempty"`
	CPU          int            `json:"cpu,omitempty"`
	MemoryMB     int            `json:"memory_mb,omitempty"`
	Hardware     map[string]int `json:"hardware,omitempty"`
}

type Step struct {
	ID         string `json:"id"`
	Command    string `json:"command"`
	WorkingDir string `json:"working_dir,omitempty"`
}

// Spec describes a reusable build, not an authorization to run its commands.
type Spec struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	Environment string            `json:"environment,omitempty"`
	SoftwareIDs []string          `json:"software_ids,omitempty"`
	Repository  string            `json:"repository,omitempty"`
	Revision    string            `json:"revision,omitempty"`
	Parameters  map[string]string `json:"parameters,omitempty"`
	Steps       []Step            `json:"steps"`
	Outputs     []string          `json:"outputs,omitempty"`
	Requires    Requirements      `json:"requires"`
}

type Profile struct {
	Spec
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Registry stores immutable revisions; callers cannot modify returned records.
type Registry struct {
	mu   sync.Mutex
	path string
	data map[string][]Profile
}

func Open(path string) (*Registry, error) {
	if path == "" {
		return nil, errors.New("profile storage path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	r := &Registry{path: path, data: map[string][]Profile{}}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return nil, err
	}
	if len(raw) > 4<<20 {
		return nil, errors.New("profile storage exceeds size limit")
	}
	if err := json.Unmarshal(raw, &r.data); err != nil {
		return nil, fmt.Errorf("decode profiles: %w", err)
	}
	if r.data == nil {
		return nil, errors.New("invalid profile storage")
	}
	for id, history := range r.data {
		if !identifier.MatchString(id) || len(history) == 0 {
			return nil, errors.New("invalid profile history")
		}
		for i, item := range history {
			if item.ID != id || item.Version != i+1 || validate(item.Spec) != nil {
				return nil, errors.New("invalid profile revision")
			}
		}
	}
	return r, nil
}

func validate(s Spec) error {
	if !identifier.MatchString(s.ID) || strings.TrimSpace(s.Title) == "" || len(s.Title) > 160 || len(s.Steps) == 0 || len(s.Steps) > 32 {
		return errors.New("invalid profile identity or steps")
	}
	if s.Environment != "" && !identifier.MatchString(s.Environment) {
		return errors.New("invalid environment reference")
	}
	if len(s.SoftwareIDs) > 32 || len(s.Parameters) > 64 || len(s.Outputs) > 64 || len(s.Repository) > 256 || len(s.Revision) > 128 {
		return errors.New("profile exceeds limits")
	}
	for _, id := range s.SoftwareIDs {
		if !identifier.MatchString(id) {
			return errors.New("invalid software reference")
		}
	}
	if s.Requires.Architecture != "" && s.Requires.Architecture != "amd64" && s.Requires.Architecture != "arm64" {
		return errors.New("unsupported compute architecture")
	}
	if s.Requires.CPU < 0 || s.Requires.CPU > 128 || s.Requires.MemoryMB < 0 || s.Requires.MemoryMB > 1048576 {
		return errors.New("invalid compute requirements")
	}
	for kind, count := range s.Requires.Hardware {
		if (kind != "fpga" && kind != "gpu" && kind != "npu" && kind != "usb") || count < 1 || count > 16 {
			return errors.New("invalid accelerator requirement")
		}
	}
	for name, value := range s.Parameters {
		if !fieldName.MatchString(name) || len(value) > 1024 {
			return errors.New("invalid profile parameter")
		}
		lower := strings.ToLower(name)
		if strings.Contains(lower, "secret") || strings.Contains(lower, "password") || strings.Contains(lower, "token") || strings.Contains(lower, "credential") || strings.Contains(lower, "api_key") {
			return errors.New("credential values cannot be stored as profile parameters")
		}
	}
	seen := map[string]bool{}
	for _, step := range s.Steps {
		if !identifier.MatchString(step.ID) || seen[step.ID] || strings.TrimSpace(step.Command) == "" || len(step.Command) > 4096 || !relativePath(step.WorkingDir) {
			return errors.New("invalid profile step")
		}
		seen[step.ID] = true
	}
	for _, output := range s.Outputs {
		if output == "" || !relativePath(output) {
			return errors.New("invalid profile output path")
		}
	}
	return nil
}

func relativePath(s string) bool {
	if s == "" {
		return true
	}
	if strings.HasPrefix(s, "/") || strings.Contains(s, "\\") || strings.ContainsRune(s, '\x00') || filepath.Clean(s) != s {
		return false
	}
	for _, part := range strings.Split(s, "/") {
		if part == ".." || part == "." || part == "" {
			return false
		}
	}
	return true
}

func clone(p Profile) Profile {
	p.SoftwareIDs = append([]string(nil), p.SoftwareIDs...)
	p.Steps = append([]Step(nil), p.Steps...)
	p.Outputs = append([]string(nil), p.Outputs...)
	if p.Parameters != nil {
		copy := make(map[string]string, len(p.Parameters))
		for k, v := range p.Parameters {
			copy[k] = v
		}
		p.Parameters = copy
	}
	if p.Requires.Hardware != nil {
		copy := make(map[string]int, len(p.Requires.Hardware))
		for k, v := range p.Requires.Hardware {
			copy[k] = v
		}
		p.Requires.Hardware = copy
	}
	return p
}

func (r *Registry) Save(s Spec, expectedVersion int) (Profile, error) {
	if err := validate(s); err != nil {
		return Profile{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	history := r.data[s.ID]
	if expectedVersion != len(history) {
		return Profile{}, errors.New("profile revision conflict")
	}
	now := time.Now().UTC()
	created := now
	if len(history) > 0 {
		created = history[0].CreatedAt
	}
	p := clone(Profile{Spec: s, Version: len(history) + 1, CreatedAt: created, UpdatedAt: now})
	next := make(map[string][]Profile, len(r.data)+1)
	for key, versions := range r.data {
		next[key] = versions
	}
	next[s.ID] = append(append([]Profile(nil), history...), p)
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return Profile{}, err
	}
	if len(data) > 4<<20 {
		return Profile{}, errors.New("profile storage exceeds size limit")
	}
	file, err := os.CreateTemp(filepath.Dir(r.path), ".profiles-*")
	if err != nil {
		return Profile{}, err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(0600); err != nil {
		file.Close()
		return Profile{}, err
	}
	if _, err = file.Write(data); err != nil {
		file.Close()
		return Profile{}, err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return Profile{}, err
	}
	if err = file.Close(); err != nil {
		return Profile{}, err
	}
	if err = os.Rename(file.Name(), r.path); err != nil {
		return Profile{}, err
	}
	if dir, err := os.Open(filepath.Dir(r.path)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	r.data = next
	return clone(p), nil
}

func (r *Registry) Get(id string, version int) (Profile, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	history := r.data[id]
	if len(history) == 0 {
		return Profile{}, os.ErrNotExist
	}
	if version == 0 {
		version = len(history)
	}
	if version < 1 || version > len(history) {
		return Profile{}, os.ErrNotExist
	}
	return clone(history[version-1]), nil
}

func (r *Registry) List() []Profile {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]Profile, 0, len(r.data))
	for _, history := range r.data {
		result = append(result, clone(history[len(history)-1]))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}
