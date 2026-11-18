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
   if status.ID == id && status.Phase == phase {
    if action == "activate" { return ctrl.ContinueAudited(ctx,action,id) }
    return ctrl.Continue(ctx,action,id)
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
