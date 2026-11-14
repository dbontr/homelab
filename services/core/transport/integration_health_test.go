package transport

import (
 "context"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
 "time"
)

func TestIntegrationHealthUsesOneBudgetAndKeepsLocalAvailability(t *testing.T){
 healthy:=discoveryBackend(t,"health_food_search","USDA lookup")
 defer healthy.Close()
 releaseBlocked:=make(chan struct{})
 blocked:=httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter,r *http.Request){
  select {
  case <-r.Context().Done():
  case <-releaseBlocked:
  }
 }))
 defer func(){close(releaseBlocked);blocked.Close()}()
 gateway:=discoveryServer(t,[]Backend{
  {Name:"workspace",URL:blocked.URL},
  {Name:"health",URL:healthy.URL},
 })
 ctx,cancel:=context.WithTimeout(context.Background(),160*time.Millisecond)
 defer cancel()
 start:=time.Now()
 raw,err:=gateway.localTool(ctx,"system_status",json.RawMessage("{}"))
 if err!=nil{t.Fatal(err)}
 if time.Since(start)>600*time.Millisecond{t.Fatal("failed integration delayed overall health observation")}
 status,ok:=raw.(map[string]any)
 if !ok{t.Fatalf("invalid health structure: %+v",raw)}
 services,ok:=status["integrations"].(map[string]string)
 if !ok{t.Fatalf("invalid service status map: %+v",raw)}
 if status["local"]!="available"||services["health"]!="available"||services["compute"]!="unavailable"{
  t.Fatalf("unavailable integration was reported as healthy or hid local health: %+v",status)
 }
}

func TestIntegrationHealthRejectsMalformedBackendDiscovery(t *testing.T){
 invalid:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  w.Header().Set("Content-Type","application/json")
  _,_ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"not_tools":[]}}`))
 }))
 defer invalid.Close()
 gateway:=discoveryServer(t,[]Backend{{Name:"learning",URL:invalid.URL}})
 result,err:=gateway.localTool(context.Background(),"system_status",json.RawMessage("{}"))
 if err!=nil{t.Fatal(err)}
 status:=result.(map[string]any)["integrations"].(map[string]string)
 if status["learning"]!="unavailable"{
  t.Fatalf("malformed response was accepted as a healthy learning service: %+v",status)
 }
}

func TestIntegrationStatusUsesNamespacedComputeIdentity(t *testing.T){
 healthy:=discoveryBackend(t,"pool_list_images","images")
 defer healthy.Close()
 gateway:=discoveryServer(t,[]Backend{{Name:"workspace",URL:healthy.URL}})
 result,err:=gateway.localTool(context.Background(),"system_status",json.RawMessage("{}"))
 if err!=nil{t.Fatal(err)}
 status:=result.(map[string]any)["integrations"].(map[string]string)
 if status["compute"]!="available"{
  t.Fatalf("workspace did not resolve to canonical compute namespace: %+v",status)
 }
 for key:=range status{
  if strings.HasPrefix(key,"workspace"){t.Fatal("legacy integration namespace leaked")}
 }
}
