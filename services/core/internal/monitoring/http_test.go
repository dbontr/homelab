package monitoring

import (
 "io"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/observability"
)

func view(h http.Handler,path string)*httptest.ResponseRecorder{
 w:=httptest.NewRecorder()
 h.ServeHTTP(w,httptest.NewRequest(http.MethodGet,path,nil))
 return w
}

func TestMonitoringRoutesAreReadOnlyAndUnavailableWithoutSources(t *testing.T){
 mux:=http.NewServeMux()
 Register(mux,nil)
 if w:=view(mux,"/v1/monitoring/targets");w.Code!=http.StatusServiceUnavailable{
  t.Fatalf("unconfigured monitoring unexpectedly returned %d",w.Code)
 }
 if w:=view(mux,"/mcp");w.Code!=http.StatusNotFound{
  t.Fatal("monitoring module exposed assistant transport")
 }
}

func TestMonitoringViewsEnforceFixedMetricsAndLogBounds(t *testing.T){
 upstreamCalls:=0
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  upstreamCalls++
  if r.URL.Path!="/api/v1/targets"{t.Errorf("unexpected monitoring upstream query %s",r.URL.Path)}
  w.Header().Set("Content-Type","application/json")
  _,_=io.WriteString(w,`{"status":"success","data":{"activeTargets":[]}}`)
 }))
 defer upstream.Close()
 client,err:=observability.New(upstream.URL,upstream.URL)
 if err!=nil{t.Fatal(err)}
 mux:=http.NewServeMux()
 Register(mux,client)
 for _,path:=range []string{
  "/v1/monitoring/series?metric=unapproved&range=6h",
  "/v1/monitoring/series?metric=cpu&range=90d",
  "/v1/monitoring/logs?limit=201",
 }{
  if w:=view(mux,path);w.Code!=http.StatusBadRequest{
   t.Fatalf("invalid monitor query admitted %s: %d",path,w.Code)
  }
 }
 if upstreamCalls!=0{t.Fatal("invalid monitoring request reached collectors")}
 w:=view(mux,"/v1/monitoring/targets")
 if w.Code!=http.StatusOK||!strings.Contains(w.Body.String(),"[]"){
  t.Fatalf("target view did not use configured source: %d %s",w.Code,w.Body.String())
 }
 if upstreamCalls!=1{t.Fatal("validated monitoring request did not reach upstream")}
}
