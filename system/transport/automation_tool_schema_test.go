package transport

import "testing"

func TestAutomationToolAdvertisesOptInChangeNotifications(t *testing.T) {
	var server Server
	found := false
	for _, entry := range server.localTools() {
		if entry["name"] != "automation_create" {
			continue
		}
		found = true
		schema, ok := entry["inputSchema"].(map[string]any)
		if !ok {
			t.Fatal("automation tool input schema is invalid")
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatal("automation tool properties are unavailable")
		}
		notify, ok := properties["notify_on_change"].(map[string]any)
		if !ok || notify["type"] != "boolean" {
			t.Fatal("change monitoring cannot be selected by MCP clients")
		}
		for _, required := range schema["required"].([]string) {
			if required == "notify_on_change" {
				t.Fatal("change monitoring must remain opt-in")
			}
		}
	}
	if !found {
		t.Fatal("recurring automation tool is missing")
	}
}
