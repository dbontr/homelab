package monitoring

import (
 "net/http"
 "strconv"
 "strings"

 "github.com/dbontr/homelab-new/system/internal/httpjson"
 "github.com/dbontr/homelab-new/system/observability"
)

type Handler struct{client *observability.Client}

// Register exposes bounded monitoring views through the authenticated private
// service router. Prometheus and Loki remain independent data owners.
func Register(mux *http.ServeMux,client *observability.Client){
 if mux==nil{panic("monitoring requires an HTTP router")}
 h:=&Handler{client:client}
 for _,path:=range []string{
  "/v1/monitoring/summary","/v1/monitoring/targets",
  "/v1/monitoring/alerts","/v1/monitoring/security",
  "/v1/monitoring/series","/v1/monitoring/logs",
 }{mux.HandleFunc("GET "+path,h.serve)}
}

func(h *Handler) serve(w http.ResponseWriter,r *http.Request){
 if h.client==nil {
  httpjson.Error(w,http.StatusServiceUnavailable,"monitoring is not configured")
  return
 }
 ctx:=r.Context()
 switch r.URL.Path {
 case "/v1/monitoring/summary":
  value,err:=h.client.Summary(ctx)
  if err!=nil {httpjson.Error(w,502,"Prometheus summary is unavailable");return}
  httpjson.Write(w,200,value)
 case "/v1/monitoring/targets":
  value,err:=h.client.Targets(ctx)
  if err!=nil{httpjson.Error(w,502,"Prometheus targets are unavailable");return}
  httpjson.Write(w,200,value)
 case "/v1/monitoring/alerts":
  value,err:=h.client.Alerts(ctx)
  if err!=nil{httpjson.Error(w,502,"Prometheus alerts are unavailable");return}
  httpjson.Write(w,200,value)
 case "/v1/monitoring/security":
  value,err:=h.client.SecurityAudit(ctx)
  if err!=nil{httpjson.Error(w,502,"security journal is unavailable");return}
  httpjson.Write(w,200,value)
 case "/v1/monitoring/series":
  key:=r.URL.Query().Get("metric")
  window:=r.URL.Query().Get("range")
  if window==""{window="6h"}
  allowed:="|cpu|memory|disk|gpu|gpu-vram|gpu-temp|gpu-age|network-rx|network-tx|disk-read|disk-write|availability|"
  if !strings.Contains(allowed,"|"+key+"|")|| (window!="1h"&&window!="6h"&&window!="24h"&&window!="7d"){
   httpjson.Error(w,400,"unsupported monitoring metric or range");return
  }
  value,err:=h.client.Series(ctx,key,window)
  if err!=nil{httpjson.Error(w,502,"monitoring history is unavailable");return}
  httpjson.Write(w,200,value)
 case "/v1/monitoring/logs":
  window:=r.URL.Query().Get("range")
  if window==""{window="1h"}
  search:=r.URL.Query().Get("search")
  count:=100
  if raw:=r.URL.Query().Get("limit");raw!=""{
   n,err:=strconv.Atoi(raw)
   if err!=nil||n<1||n>200{httpjson.Error(w,400,"invalid log entry limit");return}
   count=n
  }
  if window!="1h"&&window!="6h"&&window!="24h"||len(search)>120||
    strings.ContainsAny(search,"\r\n\x00"){
   httpjson.Error(w,400,"unsupported log search or time range");return
  }
  value,err:=h.client.Logs(ctx,window,search,count)
  if err!=nil{httpjson.Error(w,502,"monitoring logs are unavailable");return}
  httpjson.Write(w,200,value)
 default:
  httpjson.Error(w,404,"monitoring view not found")
 }
}
