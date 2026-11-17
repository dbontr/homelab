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
	"sort"
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
	ActiveAlerts        map[string]bool `json:"active_alerts,omitempty"`
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
		state: backupHealthState{Version: 1, StartedAt: time.Now().UTC(), ActiveAlerts: map[string]bool{}},
	}
	if raw, err := os.ReadFile(file); err == nil {
		if err := json.Unmarshal(raw, &monitor.state); err != nil ||
			monitor.state.Version != 1 || monitor.state.StartedAt.IsZero() {
			return nil, errors.New("invalid persisted backup health receipt")
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if monitor.state.ActiveAlerts == nil {
		monitor.state.ActiveAlerts = map[string]bool{}
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
		strings.Contains(message, "checksum"), strings.Contains(message, "verification failed"),
		strings.Contains(message, "content mismatch"):
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

// notifyLocked transmits a state transition only if it has not been
// acknowledged. A failed delivery remains pending for the next attempt.
func (m *backupHealthMonitor) notifyLocked(condition, action string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.notify(ctx, condition, action); err != nil {
		return err
	}
	if action == "resolved" {
		delete(m.state.ActiveAlerts, condition)
	} else {
		m.state.ActiveAlerts[condition] = true
	}
	return m.persistLocked()
}

// recordAttempt stores the latest receipt and independently manages every
// active failure condition. Alert delivery is serialized with status changes.
func (m *backupHealthMonitor) recordAttempt(snapshot string, attemptErr error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	m.state.LastAttemptAt = now
	alerts := []string{}
	if attemptErr == nil {
		m.state.LastSuccessAt = now
		m.state.LastSnapshot = snapshot
		m.state.ConsecutiveFailures = 0
		m.state.LastFailure = ""
		for condition := range m.state.ActiveAlerts {
			alerts = append(alerts, condition)
		}
		sort.Strings(alerts)
	} else {
		m.state.ConsecutiveFailures++
		m.state.LastFailure = backupFailureClass(attemptErr)
		if m.state.LastFailure == "insufficient_capacity" || m.state.LastFailure == "corrupt_snapshot" {
			alerts = append(alerts, m.state.LastFailure)
		}
		if m.state.ConsecutiveFailures >= 3 {
			alerts = append(alerts, "repeated_failures")
		}
	}
	if err := m.persistLocked(); err != nil {
		return err
	}
	var deliveryErr error
	for _, condition := range alerts {
		if attemptErr == nil {
			if err := m.notifyLocked(condition, "resolved"); err != nil {
				deliveryErr = errors.Join(deliveryErr, err)
			}
		} else if !m.state.ActiveAlerts[condition] {
			if err := m.notifyLocked(condition, "firing"); err != nil {
				deliveryErr = errors.Join(deliveryErr, err)
			}
		}
	}
	return deliveryErr
}

// checkMissed runs separately from the backup cycle. A persistent last-success
// receipt is authoritative across restarts, regardless of retry activity.
func (m *backupHealthMonitor) checkMissed(now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	since := m.state.LastSuccessAt
	if since.IsZero() {
		since = m.state.StartedAt
	}
	if now.Sub(since) <= 2*m.interval || m.state.ActiveAlerts["missed_backup"] {
		return nil
	}
	return m.notifyLocked("missed_backup", "firing")
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
