package mcp

import (
 "context"
 "encoding/json"
 "errors"

 "github.com/dbontr/homelab-new/system/health/internal/googlehealth"
)
func (s *Server) EnableGoogle(client *googlehealth.Client) {
 if client==nil{return}
 s.google=client
 s.registerGoogleTools()
}
func (s *Server) registerGoogleTools(){
 txt:=func(description string)map[string]any{return field("string",description)}
 s.add("health_google_read","Read a page of Google Health v4 observations using authorized read-only credentials. This never writes to Google.",true,false,[]string{"data_type"},
 map[string]any{"data_type":txt("Google Health data type, such as sleep, weight, heart-rate or steps"),
  "filter":txt("Optional Google Health API filter"),"page_token":txt("Optional continuation token")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  kind,err:=requiredString(a,"data_type");if err!=nil{return nil,err}
  page,err:=s.google.Page(ctx,kind,optionalString(a,"filter",""),optionalString(a,"page_token",""))
  if err!=nil{return nil,err}
  return send(page)
 })
 s.add("health_google_sync","Download authorized Google Health observations into the local database. Read-only upstream; duplicate source IDs are counted, never silently overwritten.",false,false,[]string{"data_type"},
 map[string]any{"data_type":txt("Google Health type"),
 "filter":txt("Optional time filter limiting retrieved data"),"page_token":txt("Pagination token for resuming"),
 "max_pages":field("integer","1 to 20, default 4; use continuation tokens for large imports")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  kind,err:=requiredString(a,"data_type");if err!=nil{return nil,err}
  n,err:=boundedInt(a,"max_pages",4,1,20);if err!=nil{return nil,err}
  if s.health==nil{return nil,errors.New("health database is not configured")}
  summary,err:=s.google.Sync(ctx,s.health,kind,optionalString(a,"filter",""),optionalString(a,"page_token",""),n)
  if err!=nil{return nil,err}
  return send(summary)
 })
}
