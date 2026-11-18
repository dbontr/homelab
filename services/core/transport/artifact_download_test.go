package transport

import (
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/store"
)

func TestBrowserArtifactDownloadVerifiesCompleteBinary(t *testing.T){
 id:=strings.Repeat("d",32)
 binary:=[]byte("CR3-test-input-content-is-never-transformed\x00\x01")
 sum:=sha256.Sum256(binary)
 digest:=hex.EncodeToString(sum[:])
 tampered:=false
 projectAccess:="demo"
 requests:=0
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.URL.Path=="/mcp" {
   w.Header().Set("Content-Type","application/json")
   var incoming struct {Method string `json:"method"`;Params struct {Name string `json:"name"`;Arguments struct{Project string `json:"project_id"`;ID string `json:"artifact_id"`} `json:"arguments"`} `json:"params"`}
   if err:=json.NewDecoder(r.Body).Decode(&incoming);err!=nil{t.Error(err);w.WriteHeader(http.StatusBadRequest);return}
   if incoming.Method=="tools/list"{
    _,_=w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`))
    return
   }
   if incoming.Method!="tools/call"||incoming.Params.Name!="artifact_info"||
    incoming.Params.Arguments.Project!=projectAccess||incoming.Params.Arguments.ID!=id{
    _,_=w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"isError":true,"content":[{"type":"text","text":"denied"}]}}`))
    return
   }
   _=json.NewEncoder(w).Encode(map[string]any{
    "jsonrpc":"2.0","id":1,
    "result":map[string]any{
     "isError":false,
     "structuredContent":map[string]any{"id":id,"project":projectAccess,"sha256":digest,"name":"photo.CR3","size":len(binary)},
     "content":[]map[string]any{{"type":"text","text":"artifact metadata"}},
    },
   })
   return
  }
  requests++
  if r.URL.Path!="/api/projects/"+projectAccess+"/artifacts/"+id||
   r.Header.Get("Authorization")!="Bearer backend-credential-for-testing"{
   w.WriteHeader(http.StatusForbidden);return
  }
  w.Header().Set("Content-Type","application/octet-stream")
  w.Header().Set("Content-Disposition",`attachment; filename="photo.CR3"`)
  checksum:=digest
  if tampered{checksum=strings.Repeat("0",64)}
  w.Header().Set("X-Content-SHA256",checksum)
  _,_=w.Write(binary)
 }))
 defer upstream.Close()
 db,err:=store.Open(filepath.Join(t.TempDir(),"state.json"),"UTC");if err!=nil{t.Fatal(err)}
 token:=strings.Repeat("x",40)
 server,err:=New(db,token,[]Backend{{Name:"workspace",URL:upstream.URL+"/mcp",Token:"backend-credential-for-testing"}})
 if err!=nil{t.Fatal(err)}
 endpoint:="/v1/engineering/projects/demo/artifacts/"+id+"/download"
 accepted:=request(server.Handler(),"GET",endpoint,"",token)
 if accepted.Code!=http.StatusOK||string(accepted.Body.Bytes())!=string(binary)||
  accepted.Header().Get("Content-Type")!="application/octet-stream"||
  accepted.Header().Get("X-Content-SHA256")!=digest||
  !strings.Contains(accepted.Header().Get("Content-Disposition"),"photo.CR3"){
  t.Fatalf("valid binary artifact missing: %d %s",accepted.Code,accepted.Body.String())
 }
 if got:=request(server.Handler(),"GET",endpoint,"","");got.Code!=http.StatusUnauthorized{
  t.Fatalf("binary leaked without browser authorization: %d",got.Code)
 }
 projectAccess="another"
 if got:=request(server.Handler(),"GET",endpoint,"",token);got.Code==http.StatusOK{
  t.Fatal("cross-project binary leaked")
 }
 projectAccess="demo"
 tampered=true
 got:=request(server.Handler(),"GET",endpoint,"",token)
 if got.Code==http.StatusOK||string(got.Body.Bytes())==string(binary){
  t.Fatalf("corrupt binary sent to browser: %d",got.Code)
 }
 if requests!=2{t.Fatalf("unexpected number of authenticated backend transfers: %d",requests)}
}

func TestBrowserDownloadTargetCannotEscapeConfiguredBackend(t *testing.T){
 for _,candidate:=range []string{
  "http://untrusted.example/mcp","https://user:pass@example.org/mcp",
  "https://example.org/mcp?url=https://elsewhere","https://example.org/api",
 } {
  if _,err:=workspaceArtifactURL(candidate,"demo",strings.Repeat("a",32));err==nil{
   t.Fatalf("unsafe backend accepted: %q",candidate)
  }
 }
 resolved,err:=workspaceArtifactURL("https://compute.example.org/mcp","demo",strings.Repeat("a",32))
 if err!=nil||resolved!="https://compute.example.org/api/projects/demo/artifacts/"+strings.Repeat("a",32){
  t.Fatalf("incorrect scoped target: %v %s",err,resolved)
 }
}
