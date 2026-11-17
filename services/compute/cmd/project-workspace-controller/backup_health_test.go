package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testBackupHealthMonitor(t *testing.T) (*backupHealthMonitor, *[][]string, func()) {
	t.Helper()
	var mu sync.Mutex
	events := make([][]string, 0)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-secret-token-1234" {
			t.Errorf("backup alert credential missing")
		}
		var event map[string]string
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Errorf("invalid alert request: %v", err)
		}
		mu.Lock()
		events = append(events, []string{event["condition"], event["status"]})
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	root := t.TempDir()
	tokenFile := filepath.Join(root, "alert-token")
	if err := os.WriteFile(tokenFile, []byte("synthetic-secret-token-1234"), 0600); err != nil {
		t.Fatal(err)
	}
	monitor, err := newBackupHealthMonitor(filepath.Join(root, "backup-health.json"),
		time.Hour, server.URL, tokenFile, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	results := &events
	return monitor, results, server.Close
}

func TestBackupHealthDetectsMissedScheduleAcrossRestart(t *testing.T) {
	m, events, closeServer := testBackupHealthMonitor(t)
	defer closeServer()
	m.mu.Lock()
	m.state.StartedAt = time.Now().Add(-3 * time.Hour).UTC()
	if err := m.persistLocked(); err != nil {
		t.Fatal(err)
	}
	m.mu.Unlock()
	if err := m.checkMissed(time.Now()); err != nil {
		t.Fatal(err)
	}
	reloaded, err := newBackupHealthMonitor(m.file, time.Hour, m.endpoint,
		filepath.Join(filepath.Dir(m.file), "alert-token"), m.client)
	if err != nil {
		t.Fatal(err)
	}
	m = reloaded
	if err := m.checkMissed(time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(*events) != 1 || (*events)[0][0] != "missed_backup" || (*events)[0][1] != "firing" {
		t.Fatalf("missed backup alert should fire only once: %v", *events)
	}
	raw, err := os.ReadFile(m.file)
	if err != nil {
		t.Fatal(err)
	}
	var persisted backupHealthState
	if err := json.Unmarshal(raw, &persisted); err != nil || !persisted.ActiveAlerts["missed_backup"] {
		t.Fatalf("failed to save missed backup alert: %v, %v", persisted, err)
	}
	if err := m.recordAttempt("12345678", nil); err != nil {
		t.Fatal(err)
	}
	if len(*events) != 2 || (*events)[1][0] != "missed_backup" || (*events)[1][1] != "resolved" {
		t.Fatalf("successful backup should resolve missed alert: %v", *events)
	}
}

func TestBackupHealthNotifiesRepeatedFailuresAndRecovery(t *testing.T) {
	m, events, closeServer := testBackupHealthMonitor(t)
	defer closeServer()
	for i := 0; i < 3; i++ {
		if err := m.recordAttempt("", errors.New("destination unavailable")); err != nil {
			t.Fatal(err)
		}
	}
	if len(*events) != 1 || (*events)[0][0] != "repeated_failures" {
		t.Fatalf("three consecutive failures require a notification: %v", *events)
	}
	if err := m.recordAttempt("12345678", nil); err != nil {
		t.Fatal(err)
	}
	if len(*events) != 2 || (*events)[1][1] != "resolved" {
		t.Fatalf("successful snapshot must resolve the alert: %v", *events)
	}
	if m.state.ConsecutiveFailures != 0 || m.state.LastSnapshot != "12345678" {
		t.Fatalf("backup receipt was not committed: %+v", m.state)
	}
}

func TestBackupHealthClassifiesCapacityAndCorruption(t *testing.T) {
	for _, testcase := range []struct {
		errorText string
		expected  string
	}{
		{"destination has insufficient capacity", "insufficient_capacity"},
		{"encrypted repository verification failed", "corrupt_snapshot"},
		{"network timeout", "backup_failure"},
	} {
		if got := backupFailureClass(errors.New(testcase.errorText)); got != testcase.expected {
			t.Fatalf("%q classified as %s", testcase.errorText, got)
		}
	}
}

func TestBackupHealthRejectsInsecureNotifications(t *testing.T) {
	root := t.TempDir()
	token := filepath.Join(root, "token")
	if err := os.WriteFile(token, []byte("synthetic-secret-token-1234"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := newBackupHealthMonitor(filepath.Join(root, "health.json"), time.Hour,
		"https://alerts.example.invalid/hook", token, nil)
	if err == nil || !strings.Contains(err.Error(), "owner-only") {
		t.Fatalf("insecure token must be rejected: %v", err)
	}
	if err := os.Chmod(token, 0600); err != nil {
		t.Fatal(err)
	}
	_, err = newBackupHealthMonitor(filepath.Join(root, "health.json"), time.Hour,
		"http://alerts.example.invalid/hook", token, nil)
	if err == nil {
		t.Fatal("plaintext alert URL must be rejected")
	}
}

func TestBackupSchedulerStopsOnInterruption(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	calls := make(chan struct{}, 1)
	go func() {
		defer close(done)
		runScheduledOffsiteBackups(ctx, time.Hour, func(ctx context.Context) (string, error) {
			calls <- struct{}{}
			<-ctx.Done()
			return "", ctx.Err()
		}, func(string, error) {})
	}()
	select {
	case <-calls:
		cancel()
	case <-time.After(2 * time.Second):
		t.Fatal("backup did not start")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("interrupted backup did not stop")
	}
}

func TestBackupHealthReportsConcurrentFailuresIndependently(t *testing.T) {
	m, events, closeServer := testBackupHealthMonitor(t)
	defer closeServer()
	if err := m.recordAttempt("", errors.New("backup destination has insufficient capacity")); err != nil {
		t.Fatal(err)
	}
	if err := m.recordAttempt("", errors.New("encrypted repository verification failed")); err != nil {
		t.Fatal(err)
	}
	if err := m.recordAttempt("", errors.New("destination unavailable")); err != nil {
		t.Fatal(err)
	}
	if len(*events) != 3 || !m.state.ActiveAlerts["insufficient_capacity"] ||
		!m.state.ActiveAlerts["corrupt_snapshot"] || !m.state.ActiveAlerts["repeated_failures"] {
		t.Fatalf("independent backup failures must remain active: alerts=%v events=%v", m.state.ActiveAlerts, *events)
	}
	if err := m.recordAttempt("12345678", nil); err != nil {
		t.Fatal(err)
	}
	if len(*events) != 6 || len(m.state.ActiveAlerts) != 0 {
		t.Fatalf("all active alerts must resolve on recovery: alerts=%v events=%v", m.state.ActiveAlerts, *events)
	}
}
