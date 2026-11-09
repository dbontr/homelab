package agentflow

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAutomationChangeAlertsAreDurableAndEvidencePrivate(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "workflows"))
	if err != nil { t.Fatal(err) }
	now := time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	a, err := s.CreateAutomation(Automation{
		ID: "research-review", Project: "research", NotifyOnChange: true,
		Enabled: true, EveryMinutes: 60, Program: json.RawMessage(`{"project":"research"}`),
	})
	if err != nil { t.Fatal(err) }
	makeRun := func(a Automation, result string) {
		w := Workflow{
			ID: AutomationRunID(a.ID, a.NextAt),
			Project: a.Project, CapabilityPlan: true, AutomationID: a.ID, AutomationDue: a.NextAt,
			Steps: []Step{{ID: "sources", Kind: "research_search", Input: json.RawMessage(`{}`), MaxAttempts: 1}},
		}
		if _, err := s.FireAutomation(a, w); err != nil { t.Fatal(err) }
		claim, err := s.ClaimReady(w.ID, time.Minute)
		if err != nil { t.Fatal(err) }
		if err := s.Complete(claim, json.RawMessage(result), ""); err != nil { t.Fatal(err) }
	}
	now = a.NextAt.Add(time.Minute)
	makeRun(a, `{"secret":"first"}`)
	if _, err := s.ReconcileAutomationObservations(10); err != nil { t.Fatal(err) }
	got, err := s.ListAutomationAlerts(10)
	if err != nil || len(got) != 0 { t.Fatalf("baseline emitted alert: %+v %v", got, err) }
	a, err = s.GetAutomation(a.ID)
	if err != nil { t.Fatal(err) }
	now = a.NextAt.Add(time.Minute)
	makeRun(a, `{"secret":"first"}`)
	if _, err := s.ReconcileAutomationObservations(10); err != nil { t.Fatal(err) }
	got, err = s.ListAutomationAlerts(10)
	if err != nil || len(got) != 0 { t.Fatalf("unchanged value emitted alert: %+v %v", got, err) }
	a, _ = s.GetAutomation(a.ID)
	now = a.NextAt.Add(time.Minute)
	makeRun(a, `{"secret":"second"}`)
	if _, err := s.ReconcileAutomationObservations(10); err != nil { t.Fatal(err) }
	if processed, err := s.ReconcileAutomationObservations(10); err != nil || processed != 0 {
		t.Fatalf("observation replayed: %d %v", processed, err)
	}
	reopened, err := Open(s.dir)
	if err != nil { t.Fatal(err) }
	alerts, err := reopened.ListAutomationAlerts(10)
	if err != nil || len(alerts) != 1 || alerts[0].Acknowledged {
		t.Fatalf("lost change alert: %+v %v", alerts, err)
	}
	raw, err := json.Marshal(alerts)
	if err != nil { t.Fatal(err) }
	if string(raw) == "" || containsSecret(raw) { t.Fatal("notification contained private evidence") }
	if _, err := reopened.AcknowledgeAutomationAlert(a.ID, alerts[0].WorkflowID, 0); err == nil {
		t.Fatal("missing revision accepted")
	}
	if _, err := reopened.AcknowledgeAutomationAlert(a.ID, alerts[0].WorkflowID, alerts[0].Revision); err != nil {
		t.Fatal(err)
	}
	acknowledged, _ := reopened.ListAutomationAlerts(10)
	if !acknowledged[0].Acknowledged { t.Fatal("notification acknowledgement not durable") }
}

func containsSecret(raw []byte) bool {
	for _, phrase := range []string{"first", "second"} {
		if len(raw) >= len(phrase) {
			for i := 0; i+len(phrase) <= len(raw); i++ {
				if string(raw[i:i+len(phrase)]) == phrase { return true }
			}
		}
	}
	return false
}

func TestChangeAutomationDoesNotSkipPendingObservation(t *testing.T) {
	s := testStore(t)
	now := time.Now().UTC()
	s.now = func() time.Time { return now }
	a, err := s.CreateAutomation(Automation{
		ID: "research-review", Project: "research", NotifyOnChange: true,
		Enabled: true, EveryMinutes: 60, Program: json.RawMessage(`{"project":"research"}`),
	})
	if err != nil { t.Fatal(err) }
	now = a.NextAt.Add(time.Minute)
	first := Workflow{
		ID: AutomationRunID(a.ID, a.NextAt), Project: a.Project,
		CapabilityPlan: true, AutomationID: a.ID, AutomationDue: a.NextAt,
		Steps: []Step{{ID: "read", Kind: "research_search", Input: json.RawMessage(`{}`), MaxAttempts: 1}},
	}
	if _, err := s.FireAutomation(a, first); err != nil { t.Fatal(err) }
	current, _ := s.GetAutomation(a.ID)
	now = current.NextAt.Add(time.Minute)
	second := first
	second.ID = AutomationRunID(current.ID, current.NextAt)
	second.AutomationDue = current.NextAt
	if _, err := s.FireAutomation(current, second); err == nil {
		t.Fatal("scheduled new run before previous observation settled")
	}
	if _, err := s.ReconcileAutomationObservations(10); err != nil { t.Fatal(err) }
	if _, err := s.Get(second.ID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected future run persisted: %v", err)
	}
}
