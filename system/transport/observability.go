package transport

import (
 "net/http"
 "strconv"
 "strings"

 "github.com/dbontr/homelab-new/system/observability"
)

func(s *Server) SetObservability(client *observability.Client){s.observability=client}

// Monitoring endpoints expose fixed reports and metrics. The browser cannot
// issue arbitrary PromQL, LogQL, exporter URLs, or system commands.
func(s *Server) uiMonitoring(w http.ResponseWriter,r *http.Request){
 if s.observability==nil {
  writeError(w,http.StatusServiceUnavailable,"monitoring is not configured")
  return
 }
 ctx:=r.Context()
 switch r.URL.Path {
 case "/v1/monitoring/summary":
  value,err:=s.observability.Summary(ctx)
  if err!=nil {writeError(w,502,"Prometheus summary is unavailable");return}
  writeJSON(w,200,value)
 case "/v1/monitoring/targets":
  value,err:=s.observability.Targets(ctx)
  if err!=nil{writeError(w,502,"Prometheus targets are unavailable");return}
  writeJSON(w,200,value)
 case "/v1/monitoring/alerts":
  value,err:=s.observability.Alerts(ctx)
  if err!=nil{writeError(w,502,"Prometheus alerts are unavailable");return}
  writeJSON(w,200,value)
 case "/v1/monitoring/series":
  key:=r.URL.Query().Get("metric")
  window:=r.URL.Query().Get("range")
  if window==""{window="6h"}
  allowed:="|cpu|memory|disk|gpu|gpu-vram|gpu-temp|gpu-age|network-rx|network-tx|disk-read|disk-write|availability|"
  if !strings.Contains(allowed,"|"+key+"|")|| (window!="1h"&&window!="6h"&&window!="24h"&&window!="7d"){
   writeError(w,400,"unsupported monitoring metric or range");return
  }
  value,err:=s.observability.Series(ctx,key,window)
  if err!=nil{writeError(w,502,"monitoring history is unavailable");return}
  writeJSON(w,200,value)
 case "/v1/monitoring/logs":
  window:=r.URL.Query().Get("range")
  if window==""{window="1h"}
  search:=r.URL.Query().Get("search")
  count:=100
  if raw:=r.URL.Query().Get("limit");raw!=""{
   n,err:=strconv.Atoi(raw)
   if err!=nil||n<1||n>200{writeError(w,400,"invalid log entry limit");return}
   count=n
  }
  if window!="1h"&&window!="6h"&&window!="24h"||len(search)>120||
    strings.ContainsAny(search,"\r\n\x00"){
   writeError(w,400,"unsupported log search or time range");return
  }
  value,err:=s.observability.Logs(ctx,window,search,count)
  if err!=nil{writeError(w,502,"monitoring logs are unavailable");return}
  writeJSON(w,200,value)
 default:
  writeError(w,404,"monitoring view not found")
 }
}
