package transport

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dbontr/homelab-new/system/devices"
	"github.com/dbontr/homelab-new/system/store"
)

func TestBorrowedFPGAIsNotPassedToComputeBackend(t *testing.T) {
	var forwarded atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"ok"}]}}`))
	}))
	defer upstream.Close()

	db, err := store.Open(filepath.Join(t.TempDir(), "state.json"), "UTC")
	if err != nil { t.Fatal(err) }
	server, err := New(db, strings.Repeat("e", 40), []Backend{{Name:"workspace", URL:upstream.URL}})
	if err != nil { t.Fatal(err) }
	catalog, err := devices.New([]devices.Device{{ID:"kv260", Kind:"fpga", Ownership:"borrowed", Connection:"ssh", AllowedOperations:[]string{"connect","inspect"}}})
	if err != nil { t.Fatal(err) }
	server.SetDeviceCatalog(catalog)

	token := strings.Repeat("e", 40)
	w := request(server.Handler(),"GET","/v1/devices","",token)
	if w.Code!=200 || !strings.Contains(w.Body.String(), "kv260") || !strings.Contains(w.Body.String(), "ssh") {t.Fatalf("missing device policy: %s",w.Body.String())}
	payload := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"workspace__workspace_create","arguments":{"hardware":{"fpga":1}}}}`
	w = request(server.Handler(),"POST","/mcp",payload,token)
	if forwarded.Load()!=0 || !strings.Contains(w.Body.String(),"FPGA allocation requires") {t.Fatalf("borrowed FPGA sent to backend: %s",w.Body.String())}
	payload = `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"workspace__workspace_create","arguments":{"usb_passthrough":true}}}`
	w = request(server.Handler(),"POST","/mcp",payload,token)
	if forwarded.Load()!=0 || !strings.Contains(w.Body.String(),"USB devices") {t.Fatalf("USB passthrough sent to backend: %s",w.Body.String())}
	payload = `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"device_inventory","arguments":{}}}`
	w = request(server.Handler(),"POST","/mcp",payload,token)
	if !strings.Contains(w.Body.String(),"kv260") {t.Fatalf("inventory not exposed: %s",w.Body.String())}
}
