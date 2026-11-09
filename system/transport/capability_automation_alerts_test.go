package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestChangeAutomationRoutesArePrivateAndRevisionFenced(t *testing.T) {
	s, _, _ := flowFixture(t)
	a, err := s.createCapabilityAutomation(context.Background(), AutomationRequest{
		EveryMinutes: 60,
		Enabled: true,
		NotifyOnChange: true,
		Program: CapabilityProgram{
			Project: "study",
			Objective: "Detect changes in research",
			Steps: []CapabilityStage{{ID: "sources", Action: "research_search", Query: "electron"}},
		},
	})
	if err != nil || !a.NotifyOnChange { t.Fatalf("change monitoring rejected: %+v %v", a, err) }
	h := s.Handler()
	token := strings.Repeat("z", 40)
	if got := request(h, "GET", "/v1/automations/alerts", "", ""); got.Code != 401 {
		t.Fatalf("anonymous alert listing accepted: %d", got.Code)
	}
	if got := request(h, "GET", "/v1/automations/alerts", "", token); got.Code != 200 ||
		!strings.Contains(got.Body.String(), `"alerts"`) {
		t.Fatalf("authenticated alert listing failed: %d %s", got.Code, got.Body.String())
	}
	if got := request(h, "POST", "/v1/automations/"+a.ID+"/alerts/unknown/ack",
		`{"revision":1}`, token); got.Code != 404 {
		t.Fatalf("unknown alert was acknowledged: %d %s", got.Code, got.Body.String())
	}
	if _, err := s.capabilityAutomationTool(context.Background(), "automation_alerts", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("MCP alert tool unavailable: %v", err)
	}
	if _, err := s.capabilityAutomationTool(context.Background(), "automation_alert_acknowledge",
		json.RawMessage(`{"automation_id":"nonexistent","workflow_id":"run","revision":1}`)); err == nil {
		t.Fatal("MCP acknowledged nonexistent alert")
	}
}
