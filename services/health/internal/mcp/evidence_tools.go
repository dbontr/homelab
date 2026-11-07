package mcp

import(
 "context"
 "encoding/json"
 "errors"
)
func(s *Server)registerEvidenceTools(){
 txt:=func(d string)map[string]any{return field("string",d)}
 s.add("health_evidence_add","Store an attached photograph, label, or laboratory PDF locally as source evidence. Does not send data to external providers.",false,false,
 []string{"filename","mime","base64"},
 map[string]any{"filename":txt("Filename without directory components"),
  "mime":selectField("Media type","image/jpeg","image/png","image/webp","image/heic","application/pdf"),
  "base64":txt("Base64 file payload, maximum 2 MiB decoded"),"source":txt("Data provenance, defaults to manual"),
  "description":txt("Document or image description without inferred diagnoses")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  name,err:=requiredString(a,"filename");if err!=nil{return nil,err}
  mime,err:=requiredString(a,"mime");if err!=nil{return nil,err}
  data,err:=requiredString(a,"base64");if err!=nil{return nil,err}
  src:=optionalString(a,"source","manual")
  if src==""{src="manual"}
  evidence,err:=s.health.AddEvidence(ctx,name,mime,data,src,optionalString(a,"description",""))
  if err!=nil{return nil,err}
  return send(evidence)
 })
 s.add("health_evidence_get","Read evidence metadata; only return the original base64 payload when explicitly requested.",true,false,[]string{"id"},
 map[string]any{"id":txt("Evidence record ID"),"include_base64":field("boolean","Return file bytes; defaults to false")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  id,err:=requiredString(a,"id");if err!=nil{return nil,err}
  include:=false
  if len(a["include_base64"])>0&&json.Unmarshal(a["include_base64"],&include)!=nil{
   return nil,errors.New("include_base64 must be boolean")
  }
  e,encoded,err:=s.health.GetEvidence(ctx,id,include)
  if err!=nil{return nil,err}
  if include{return send(map[string]any{"evidence":e,"base64":encoded})}
  return send(e)
 })
 s.add("health_evidence_list","List stored original documents and photographs with MIME type and checksum.",true,false,nil,
 map[string]any{"limit":field("integer","1 to 100, default 25")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  n,err:=boundedInt(a,"limit",25,1,100);if err!=nil{return nil,err}
  list,err:=s.health.ListEvidence(ctx,n);if err!=nil{return nil,err}
  return send(list)
 })
}
