package main

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "os"
 "strings"
 "time"

 "github.com/dbontr/homelab-new/system/canonicaljson"
 "github.com/dbontr/homelab-new/system/privileged"
 "github.com/dbontr/homelab-new/system/release"
)

// guardRelease runs only in a detached, root-owned service. Authorization is
// proved by the executor's durable audit entry for the exact stage and host.
func guardRelease(ctx context.Context, ctrl *release.Controller, action, requestID, id string) error {
 if !validRequestID(requestID) || !validStagedID(id) { return errors.New("invalid protected release identity") }
 if action != "activate" && action != "revert" { return errors.New("invalid protected release action") }
 auditPath := os.Getenv("HOMELAB_RELEASE_AUDIT_FILE")
 if auditPath == "" { return errors.New("protected executor audit file required") }
 raw, err := json.Marshal(map[string]string{"host":ctrl.Config.Host,"release_id":id})
 if err != nil { return err }
 canonical, _, err := canonicaljson.Object(raw)
 if err != nil { return err }
 sum := sha256.Sum256(canonical)
 digest := hex.EncodeToString(sum[:])
 operation := "infrastructure__release_"+action
 tick := time.NewTicker(250*time.Millisecond)
 defer tick.Stop()
 for {
  outcome, found, err := releaseAuditOutcome(auditPath, requestID, operation, ctrl.Config.Host, digest)
  if err != nil { return err }
  switch {
  case found && outcome == "accepted":
   if action == "activate" { return ctrl.Activate(ctx,id) }
   return ctrl.Revert(ctx,id)
  case found && outcome == "started":
   status, err := ctrl.Status()
   if err != nil { return err }
   phase := "activating"
   if action == "revert" { phase = "reverting" }
   if status.ID == id {
    if status.Phase == phase {
     if action == "activate" { return ctrl.ContinueAudited(ctx,action,id) }
     return ctrl.Continue(ctx,action,id)
    }
    // A completed, rolled-back or conflicting release must not keep the
    // detached guardian alive or permit an audited request to be replayed.
    if status.Phase == "rolled-back" || status.Phase == "rollback-failed" ||
       (action == "activate" && (status.Phase == "active" || status.Phase == "reverting")) {
     return errors.New("release journal no longer permits the requested transition")
    }
   }
  case found:
   return errors.New("release audit does not permit an execution")
  }
  select {
  case <-ctx.Done(): return fmt.Errorf("release authorization never reached an executable state: %w",ctx.Err())
  case <-tick.C:
  }
 }
}

func releaseAuditOutcome(path,requestID,operation,host,digest string)(string,bool,error){
 audit,err:=privileged.OpenAuditJournal(path)
 if err!=nil{return "",false,err}
 defer audit.Close()
 return audit.RequestOutcome(requestID,operation,host,digest)
}

func validRequestID(id string)bool { return lowerHex(id,32) }
func validStagedID(id string)bool { return lowerHex(id,48) }
func lowerHex(id string, length int)bool {
 if len(id)!=length || id!=strings.ToLower(id) { return false }
 for _,r:=range id { if (r<'0'||r>'9')&&(r<'a'||r>'f') { return false } }
 return true
}

func resumeAuditedActivation(ctx context.Context,ctrl *release.Controller)error{
 if ctrl==nil||!ctrl.Config.TrustedSource {
  return errors.New("automatic release handoff requires a trusted release guardian")
 }
 journal,err:=ctrl.Status()
 if errors.Is(err,os.ErrNotExist){return nil}
 if err!=nil{return err}
 if journal.Phase!="activating"{return nil}
 if !validStagedID(journal.ID){return errors.New("interrupted release identity is invalid")}
 auditPath:=os.Getenv("HOMELAB_RELEASE_AUDIT_FILE")
 if auditPath==""{return errors.New("protected release audit unavailable")}
 raw,err:=json.Marshal(map[string]string{
  "host":ctrl.Config.Host,"release_id":journal.ID,
 })
 if err!=nil{return err}
 canonical,_,err:=canonicaljson.Object(raw)
 if err!=nil{return err}
 digest:=sha256.Sum256(canonical)
 expected:=hex.EncodeToString(digest[:])
 audit,err:=privileged.OpenAuditJournal(auditPath)
 if err!=nil{return err}
 pending,err:=audit.Unresolved()
 closeErr:=audit.Close()
 if err!=nil{return err}
 if closeErr!=nil{return closeErr}
 var id string
 for _,entry:=range pending{
  if entry.Operation!="infrastructure__release_activate" ||
   entry.Target!=ctrl.Config.Host || entry.ArgumentsSHA256!=expected ||
   entry.Outcome!="started" {continue}
  if id!="" {return errors.New("ambiguous interrupted owner-approved release")}
  id=entry.ID
 }
 if id==""{
  return errors.New("interrupted release lacks an exact owner-approved audit start")
 }
 return guardRelease(ctx,ctrl,"activate",id,journal.ID)
}
