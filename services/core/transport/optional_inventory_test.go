package transport

import (
    "context"
    "encoding/json"
    "net/http"
    "path/filepath"
    "strings"
    "testing"

    "github.com/dbontr/homelab-new/system/devices"
    "github.com/dbontr/homelab-new/system/store"
)

func TestOptionalInventoriesAreUnavailableWithoutOperatorConfiguration(t *testing.T) {
    db, err := store.Open(filepath.Join(t.TempDir(), "state.json"), "UTC")
    if err != nil { t.Fatal(err) }
    token := strings.Repeat("i", 40)
    server, err := New(db, token, nil)
    if err != nil { t.Fatal(err) }
    handler := server.Handler()

    for _, path := range []string{"/v1/devices", "/v1/nodes"} {
        response := request(handler, http.MethodGet, path, "", token)
        if response.Code != http.StatusServiceUnavailable {
            t.Fatalf("%s without configuration returned %d: %s", path, response.Code, response.Body.String())
        }
    }

    list := request(handler, http.MethodPost, "/mcp",
        `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, token)
    if list.Code != http.StatusOK { t.Fatalf("tool discovery: %d", list.Code) }
    for _, name := range []string{"device_inventory", "node_inventory"} {
        if strings.Contains(list.Body.String(), `"name":"`+name+`"`) {
            t.Fatalf("unconfigured inventory %s was advertised", name)
        }
        response := server.invoke(context.Background(), name, json.RawMessage(`{}`), json.RawMessage("1"))
        if response["isError"] != true {
            t.Fatalf("unconfigured inventory %s returned a result: %+v", name, response)
        }
    }
    for _, name := range []string{
        "workspace__resource_plan", "workspace__workspace_create",
        "workspace__task_compute_submit",
    } {
        response := server.invoke(context.Background(), name, json.RawMessage(`{}`), json.RawMessage("1"))
        if response["isError"] != true ||
            !strings.Contains(response["content"].([]any)[0].(map[string]string)["text"], "device policy catalog is not configured") {
            t.Fatalf("missing device policy did not fail closed for %s: %+v", name, response)
        }
    }
}

func TestConfiguredInventoriesRemainAvailableToNativeAndMCPClients(t *testing.T) {
    db, err := store.Open(filepath.Join(t.TempDir(), "state.json"), "UTC")
    if err != nil { t.Fatal(err) }
    token := strings.Repeat("i", 40)
    server, err := New(db, token, nil)
    if err != nil { t.Fatal(err) }

    deviceCatalog, err := devices.New([]devices.Device{{
        ID: "kv260", Kind: "fpga", Mode: devices.ModeSSH,
        Connection: "ssh", AllowedOperations: []string{"connect", "inspect"},
    }})
    if err != nil { t.Fatal(err) }
    nodeCatalog, err := devices.NewNodes([]devices.Node{{ID: "external", Mode: devices.ModeSSH}})
    if err != nil { t.Fatal(err) }
    server.SetDeviceCatalog(deviceCatalog)
    server.SetNodeCatalog(nodeCatalog)

    handler := server.Handler()
    for _, tc := range []struct{path, evidence string}{
        {"/v1/devices", "kv260"}, {"/v1/nodes", "external"},
    } {
        response := request(handler, http.MethodGet, tc.path, "", token)
        if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), tc.evidence) {
            t.Fatalf("configured %s did not return owner records: %d %s", tc.path, response.Code, response.Body.String())
        }
    }
    list := request(handler, http.MethodPost, "/mcp",
        `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, token)
    for _, name := range []string{"device_inventory", "node_inventory"} {
        if !strings.Contains(list.Body.String(), `"name":"`+name+`"`) {
            t.Fatalf("configured inventory %s missing from discovery: %s", name, list.Body.String())
        }
    }
}
