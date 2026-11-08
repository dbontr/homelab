package approval

import (
 "encoding/json"
 "testing"
)

func TestApprovalChallengeRejectsDuplicateKeysAndNumericCollision(t *testing.T){
 gate:=New()
 if _,_,err:=gate.Check("cluster__configure",json.RawMessage(`{"node":1,"node":2}`));err==nil{t.Fatal("duplicate JSON key accepted")}
 a:=json.RawMessage(`{"index":9007199254740992}`)
 b:=json.RawMessage(`{"index":9007199254740993}`)
 _,challenge,err:=gate.Check("cluster__configure",a);if err!=nil{t.Fatal(err)}
 if _,err:=gate.Approve(challenge.ID);err!=nil{t.Fatal(err)}
 allowed,_,err:=gate.Check("cluster__configure",b)
 if err!=nil||allowed{t.Fatal("changed large integer was approved")}
}
