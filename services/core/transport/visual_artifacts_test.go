package transport

import (
 "bytes"
 "crypto/sha256"
 "encoding/base64"
 "encoding/hex"
 "encoding/json"
 "image"
 "image/png"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/store"
)

func TestVisualArtifactPreviewRequiresProjectAndValidDigest(t *testing.T){
 var original bytes.Buffer
 if err:=png.Encode(&original,image.NewRGBA(image.Rect(0,0,64,48)));err!=nil{t.Fatal(err)}
 data:=original.Bytes()
 sum:=sha256.Sum256(data)
 digest:=hex.EncodeToString(sum[:])
 id:=strings.Repeat("b",32)
 corrupt:=false
 calls:=0
 backend:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  var req struct{
   Method string `json:"method"`
   Params struct{
    Name string `json:"name"`
    Arguments struct{
     Project string `json:"project_id"`
     ID string `json:"artifact_id"`
    } `json:"arguments"`
   } `json:"params"`
  }
  if err:=json.NewDecoder(r.Body).Decode(&req);err!=nil{t.Error(err);w.WriteHeader(400);return}
  w.Header().Set("Content-Type","application/json")
  if req.Method=="tools/list"{
   _,_=w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`))
   return
  }
  calls++
  if req.Params.Name!="artifact_view_image"||req.Params.Arguments.Project!="demo"||req.Params.Arguments.ID!=id{
   t.Errorf("upstream image request escaped project scope: %+v",req)
   w.WriteHeader(400);return
  }
  gotDigest:=digest
  if corrupt{gotDigest=strings.Repeat("0",64)}
  _=json.NewEncoder(w).Encode(map[string]any{
   "jsonrpc":"2.0","id":1,
   "result":map[string]any{
    "isError":false,
    "structuredContent":map[string]any{
     "artifact_id":id,"sha256":gotDigest,"mime_type":"image/png","width":64,"height":48,
    },
    "content":[]map[string]any{{"type":"image","mimeType":"image/png","data":base64.StdEncoding.EncodeToString(data)}},
   },
  })
 }))
 defer backend.Close()
 db,err:=store.Open(filepath.Join(t.TempDir(),"state.json"),"UTC");if err!=nil{t.Fatal(err)}
 token:=strings.Repeat("z",40)
 server,err:=New(db,token,[]Backend{{Name:"workspace",URL:backend.URL}});if err!=nil{t.Fatal(err)}
 target:="/v1/engineering/projects/demo/artifacts/"+id+"/image"
 ok:=request(server.Handler(),"GET",target,"",token)
 if ok.Code!=http.StatusOK||!bytes.Equal(ok.Body.Bytes(),data)||ok.Header().Get("Content-Type")!="image/png"||
  ok.Header().Get("Cache-Control")!="private, no-store"||ok.Header().Get("X-Content-SHA256")!=digest {
  t.Fatalf("visual preview invalid: %d %v",ok.Code,ok.Body.String())
 }
 if noAuth:=request(server.Handler(),"GET",target,"","");noAuth.Code!=http.StatusUnauthorized{
  t.Fatalf("unauthorized screenshot: %d",noAuth.Code)
 }
 if invalid:=request(server.Handler(),"GET","/v1/engineering/projects/../artifacts/"+id+"/image","",token);invalid.Code==http.StatusOK {
  t.Fatal("invalid project scope accepted")
 }
 if calls!=1{t.Fatalf("unexpected backend preview requests: %d",calls)}
 corrupt=true
 rejected:=request(server.Handler(),"GET",target,"",token)
 if rejected.Code==http.StatusOK||bytes.Equal(rejected.Body.Bytes(),data){
  t.Fatalf("corrupt screenshot exposed: %d",rejected.Code)
 }
}
