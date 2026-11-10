package devices

import (
 "net/http"

 "github.com/dbontr/homelab-new/system/internal/httpjson"
)

// Register attaches read-only inventory resources to the authenticated
// service router; hardware authorization remains with the executor.
func Register(mux *http.ServeMux, current func()(*Catalog,*NodeCatalog)){
 if mux==nil||current==nil{panic("inventory routes require a router and inventory source")}
 mux.HandleFunc("GET /v1/devices",func(w http.ResponseWriter,_ *http.Request){
  devices,_:=current()
  if devices==nil{httpjson.Error(w,http.StatusServiceUnavailable,"device catalog is not configured");return}
  httpjson.Write(w,http.StatusOK,devices.List())
 })
 mux.HandleFunc("GET /v1/nodes",func(w http.ResponseWriter,_ *http.Request){
  _,nodes:=current()
  if nodes==nil{httpjson.Error(w,http.StatusServiceUnavailable,"node catalog is not configured");return}
  httpjson.Write(w,http.StatusOK,nodes.List())
 })
}
