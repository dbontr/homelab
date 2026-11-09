package security

import (
 "encoding/json"
 "strings"
 "path/filepath"
 "testing"
 "time"
)

func TestApprovalGrantBindsExactLargeInteger(t *testing.T){
 auth,err:=NewAuthority([]byte(strings.Repeat("x",32)),filepath.Join(t.TempDir(),"spent.json"))
 if err!=nil{t.Fatal(err)}
 a:=json.RawMessage(`{"revision":9007199254740992}`)
 b:=json.RawMessage(`{"revision":9007199254740993}`)
 grant,err:=auth.Issue("release.modify","repo",a,time.Minute)
 if err!=nil{t.Fatal(err)}
 if err:=auth.Consume(grant,"release.modify","repo",b);err==nil{t.Fatal("changed integer passed authorization")}
 if err:=auth.Consume(grant,"release.modify","repo",a);err!=nil{t.Fatal(err)}
}
