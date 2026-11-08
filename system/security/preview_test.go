package security

import (
 "encoding/json"
 "testing"
 "time"
)

func TestPreviewDoesNotConsumeConfirmedReleaseAuthorization(t *testing.T){
 manager,key,now:=testSessionManager(t)
 loginAt(t,manager,key,now)
 args:=json.RawMessage(`{"host":"saturn"}`)
 op:="cluster__configure"
 first:=manager.PreviewFor("local-operator","platform-gateway",op,"saturn",args)
 if first.Allowed||first.ConfirmationID==""{t.Fatal("destructive preview omitted confirmation",first)}
 again:=manager.PreviewFor("local-operator","platform-gateway",op,"saturn",args)
 if again.ConfirmationID!=first.ConfirmationID{t.Fatal("repeated preview created another challenge")}
 *now=now.Add(30*time.Second)
 if err:=manager.ConfirmFor("local-operator",first.ConfirmationID,testOwnerPassword,TOTPCode(key,now.Unix()/30));err!=nil{t.Fatal(err)}
 for i:=0;i<2;i++ {
  if check:=manager.PreviewFor("local-operator","platform-gateway",op,"saturn",args);!check.Allowed{
   t.Fatal("preview consumed previously confirmed challenge",i,check)
  }
 }
 if decision:=manager.AuthorizeFor("local-operator","platform-gateway",op,"saturn",args);!decision.Allowed{
  t.Fatal("confirmed action was unavailable",decision)
 }
 if replay:=manager.AuthorizeFor("local-operator","platform-gateway",op,"saturn",args);replay.Allowed{
  t.Fatal("one-time confirmation replay accepted")
 }
 manager.Revoke()
 if preview:=manager.PreviewFor("local-operator","platform-gateway",op,"saturn",args);preview.Allowed{
  t.Fatal("revoked session approved a preview")
 }
}

func TestPreviewDoesNotExtendIdleSessionOrPermitScopeSubstitution(t *testing.T){
 manager,key,now:=testSessionManager(t)
 loginAt(t,manager,key,now)
 args:=json.RawMessage(`{"project":"jupiter"}`)
 *now=now.Add(119*time.Minute)
 if p:=manager.PreviewFor("local-operator","platform-gateway","workspace__workspace_create","jupiter",args);!p.Allowed{
  t.Fatal("routine preview unexpectedly denied",p)
 }
 if p:=manager.PreviewFor("local-operator","platform-gateway","workspace__workspace_create","venus",args);!p.Allowed{
  t.Fatal("configured alternate target unavailable",p)
 }
 if p:=manager.PreviewFor("local-operator","platform-gateway","cluster__configure","venus",args);p.Allowed{
  t.Fatal("preview bypassed operation-specific resource policy")
 }
 *now=now.Add(2*time.Minute)
 if manager.Status().Active{t.Fatal("non-consuming previews extended idle expiration")}
}
