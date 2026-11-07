package canonicaljson

import (
 "encoding/json"
 "strings"
 "testing"
)

func TestExactNumberCanonicalization(t *testing.T){
 a,_,err:=Object(json.RawMessage(`{"count":9007199254740992,"nested":{"x":1}}`))
 if err!=nil{t.Fatal(err)}
 b,_,err:=Object(json.RawMessage(`{"nested":{"x":1},"count":9007199254740993}`))
 if err!=nil{t.Fatal(err)}
 if string(a)==string(b){t.Fatal("distinct large integers collapsed")}
 c,_,err:=Object(json.RawMessage(`{"nested":{"x":1},"count":9007199254740992}`))
 if err!=nil||string(a)!=string(c){t.Fatalf("equivalent objects differ: %s %s",a,c)}
}
func TestAmbiguousOrExcessiveJSONRejected(t *testing.T){
 for _,v:=range []string{
  `{"a":1,"a":2}`,
  `{"nested":[{"id":7,"id":8}]}`,
  `{"a":1} {"b":2}`,
  `[1,2]`,
  `null`,
  `{"a":}`,
  strings.Repeat(" ",(1<<20)+1),
 }{
  if _,_,err:=Object(json.RawMessage(v));err==nil{t.Fatalf("ambiguous JSON accepted: %.80q",v)}
 }
}
