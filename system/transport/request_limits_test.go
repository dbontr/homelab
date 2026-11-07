package transport

import (
 "encoding/json"
 "path/filepath"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/store"
)

func TestConstructorRejectsWeakGatewayToken(t *testing.T){
 db,err:=store.Open(filepath.Join(t.TempDir(),"state.json"),"UTC")
 if err!=nil{t.Fatal(err)}
 if _,err:=New(db,"short",nil);err==nil{t.Fatal("weak token accepted")}
 if _,err:=New(db,strings.Repeat("x",32),nil);err!=nil{t.Fatal(err)}
}

func TestMCPRejectsMultipleJSONMessages(t *testing.T){
 h:=makeTestServer(t,nil)
 token:=strings.Repeat("a",40)
 payload:=`{"jsonrpc":"2.0","id":1,"method":"ping"} {"jsonrpc":"2.0","id":2,"method":"ping"}`
 response:=request(h,"POST","/mcp",payload,token)
 var envelope struct { Error struct {Code int `json:"code"`} `json:"error"` }
 if err:=json.Unmarshal(response.Body.Bytes(),&envelope);err!=nil{t.Fatal(err)}
 if envelope.Error.Code!=-32700{t.Fatalf("multiple envelopes were accepted: %s",response.Body.String())}
}
