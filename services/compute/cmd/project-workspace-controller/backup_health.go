package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type backupHealthState struct {
	Version             int       `json:"version"`
	StartedAt           time.Time `json:"started_at"`
	LastAttemptAt       time.Time `json:"last_attempt_at,omitempty"`
	LastSuccessAt       time.Time `json:"last_success_at,omitempty"`
	LastFullCheckAt     time.Time `json:"last_full_check_at,omitempty"`
	LastSnapshot        string    `json:"last_snapshot,omitempty"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	LastFailure         string    `json:"last_failure,omitempty"`
	ActiveAlert         string    `json:"active_alert,omitempty"`
}

type backupHealthMonitor struct {
	mu       sync.Mutex
	file     string
	interval time.Duration
	endpoint string
	token    string
	client   *http.Client
	state    backupHealthState
}

func newBackupHealthMonitor(file string, interval time.Duration, endpoint, tokenFile string, client *http.Client) (*backupHealthMonitor, error) {
	if interval < time.Hour || !filepath.IsAbs(file) {
		return nil, errors.New("backup health requires an absolute state path and an interval of at least one hour")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return nil, errors.New("backup alerts require a dedicated HTTPS endpoint")
	}
	if !filepath.IsAbs(tokenFile) {
		return nil, errors.New("backup alert token requires an absolute file path")
	}
	info, err := os.Lstat(tokenFile)
	if err != nil {
		return nil, fmt.Errorf("backup alert credential unavailable: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() == 0 {
		return nil, errors.New("backup alert credential must be a nonempty owner-only regular file")
	}
	raw, err := os.ReadFile(tokenFile)
	if err != nil {
		return nil, err
	}
	token := strings.TrimSuffix(string(raw), "\n")
	if len(token) < 16 || strings.ContainsAny(token, "\x00\r\n") {
		return nil, errors.New("invalid backup alert credential")
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}}
	}
	monitor := &backupHealthMonitor{
		file: file, interval: interval, endpoint: endpoint, token: token, client: client,
		state: backupHealthState{Version: 1, StartedAt: time.Now().UTC()},
	}
	if raw, err := os.ReadFile(file); err == nil {
		if err := json.Unmarshal(raw, &monitor.state); err != nil ||
			monitor.state.Version != 1 || monitor.state.StartedAt.IsZero() {
			return nil, errors.New("invalid persisted backup health receipt")
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err := monitor.persistLocked(); err != nil {
		return nil, err
	}
	return monitor, nil
}

func (m *backupHealthMonitor) persistLocked() error {
	dir := filepath.Dir(m.file)
	temp, err := os.CreateTemp(dir, ".compute-backup-health-")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if err := temp.Chmod(0600); err != nil {
		temp.Close()
		return err
	}
	if err := json.NewEncoder(temp).Encode(m.state); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), m.file)
}

func (m *backupHealthMonitor) notify(ctx context.Context, condition, state string) error {
	payload, err := json.Marshal(map[string]string{
		"source": "compute-backup", "condition": condition,
		"status": state, "observed_at": time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+m.token)
	response, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1024))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("backup alert receiver returned HTTP %d", response.StatusCode)
	}
	return nil
}

func backupFailureClass(err error) string {
	if err == nil {
		return ""
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "insufficient capacity"), strings.Contains(message, "no space left"):
		return "insufficient_capacity"
	case strings.Contains(message, "integrity"), strings.Contains(message, "corrupt"),
		strings.Contains(message, "checksum"), strings.Contains(message, "verification failed"):
		return "corrupt_snapshot"
	default:
		return "backup_failure"
	}
}

func (m *backupHealthMonitor) lastFullCheckDue(now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state.LastFullCheckAt.IsZero() || now.Sub(m.state.LastFullCheckAt) >= 7*24*time.Hour
}

func (m *backupHealthMonitor) fullCheckSucceeded() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.LastFullCheckAt = time.Now().UTC()
	return m.persistLocked()
}

// recordAttempt persists a nonsecret receipt and notifies only on alert state
// transitions. The receiver must acknowledge the alert before it is marked sent.
func (m *backupHealthMonitor) recordAttempt(snapshot string, attemptErr error) error {
	m.mu.Lock()
	now := time.Now().UTC()
	m.state.LastAttemptAt = now
	alert, action := "", ""
	if attemptErr == nil {
		m.state.LastSuccessAt = now
		m.state.LastSnapshot = snapshot
		m.state.ConsecutiveFailures = 0
		m.state.LastFailure = ""
		if m.state.ActiveAlert != "" {
			alert, action = m.state.ActiveAlert, "resolved"
		}
	} else {
		m.state.ConsecutiveFailures++
		m.state.LastFailure = backupFailureClass(attemptErr)
		if m.state.LastFailure == "insufficient_capacity" || m.state.LastFailure == "corrupt_snapshot" {
			alert = m.state.LastFailure
		} else if m.state.ConsecutiveFailures >= 3 {
			alert = "repeated_failures"
		}
		if alert != "" && alert != m.state.ActiveAlert {
			action = "firing"
		}
	}
	persistErr := m.persistLocked()
	m.mu.Unlock()
	if persistErr != nil {
		return persistErr
	}
	if action == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.notify(ctx, alert, action); err != nil {
		return err
	}
	m.mu.Lock()
	if action == "resolved" {
		if m.state.ActiveAlert == alert {
			m.state.ActiveAlert = ""
		}
	} else {
		m.state.ActiveAlert = alert
	}
	err := m.persistLocked()
	m.mu.Unlock()
	return err
}

// checkMissed is independent of the backup retry loop. A successful receipt
// must not be older than twice the configured schedule, including across restarts.
func (m *backupHealthMonitor) checkMissed(now time.Time) error {
	m.mu.Lock()
	since := m.state.LastSuccessAt
	if since.IsZero() {
		since = m.state.StartedAt
	}
	missing := now.Sub(since) > 2*m.interval
	pending := missing && m.state.ActiveAlert != "missed_backup"
	m.mu.Unlock()
	if !pending {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.notify(ctx, "missed_backup", "firing"); err != nil {
		return err
	}
	m.mu.Lock()
	m.state.ActiveAlert = "missed_backup"
	err := m.persistLocked()
	m.mu.Unlock()
	return err
}

func (m *backupHealthMonitor) watch(ctx context.Context) {
	delay := m.interval / 4
	if delay > time.Hour {
		delay = time.Hour
	}
	if delay < time.Minute {
		delay = time.Minute
	}
	ticker := time.NewTicker(delay)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := m.checkMissed(now); err != nil {
				log.Printf("backup missed-schedule alert delivery failed: %v", err)
			}
		}
	}
}
