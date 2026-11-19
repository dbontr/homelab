package release

import (
 "context"
 "errors"
 "fmt"
 "net/url"
 "os"
 "strings"
 "time"
)

// Continue completes a recorded generation transition after executor replacement.
func (c *Controller) Continue(ctx context.Context, action, id string) error {
 return c.continueTransition(ctx, action, id, false)
}

// ContinueAudited imports an already-started transition from an executor that
// recorded its protected journal but not an independent attempt receipt. The
// caller must first verify the exact request, operation, host and stage against
// the privileged executor's audit journal. It cannot start a new transition.
func (c *Controller) ContinueAudited(ctx context.Context, action, id string) error {
 if action != "activate" { return errors.New("audited handoff supports activation only") }
 return c.continueTransition(ctx, action, id, true)
}

func (c *Controller) continueTransition(ctx context.Context, action, id string, audited bool) error {
 if !idPattern.MatchString(id) { return errors.New("invalid release ID") }
 if action != "activate" && action != "revert" { return errors.New("unsupported release transition") }
 return c.locked(func() error {
  stage, err := c.loadStage(id)
  if err != nil { return err }
  var ready Manifest
  receiptErr := c.read("stage-ready-"+id+".json", &ready)
  if receiptErr != nil {
   if !audited || !c.Config.TrustedSource || !errors.Is(receiptErr,os.ErrNotExist){
    return fmt.Errorf("stage authority receipt unavailable: %w",receiptErr)
   }
  } else if ready.ID != stage.ID || ready.Host != stage.Host || ready.Revision != stage.Revision ||
   ready.System != stage.System || !ready.Created.Equal(stage.Created) {
   return errors.New("stage authority receipt differs from immutable manifest")
  }
  var txn Journal
  if err := c.read("journal.json", &txn); err != nil { return err }
  if txn.ID != id || txn.Target != stage.System || txn.Previous == txn.Target ||
   !checkStorePath(txn.Previous) || !checkStorePath(txn.Target) || txn.Time.IsZero() {
   return errors.New("release journal does not match staged generation")
  }
  expected := "activating"
  if action == "revert" { expected = "reverting" }
  if txn.Phase != expected { return errors.New("release is not interrupted in the required phase") }
  current, err := c.Config.Current()
  if err != nil || (current != txn.Previous && current != txn.Target) {
   return errors.New("installed generation differs from the interrupted release")
  }
  var attempted Journal
  if err := c.read("attempt-"+id+".json", &attempted); err != nil {
   if !audited || !errors.Is(err, os.ErrNotExist) {
    return fmt.Errorf("release attempt not recorded: %w", err)
   }
   // Audited executor start, exact immutable stage and interrupted journal
   // together prove that the transition already began; record its attempt
   // before continuing so subsequent execution cannot replay this stage.
   attempted = txn
   if err := c.atomic("attempt-"+id+".json", attempted); err != nil { return err }
  }
  if attempted.ID != txn.ID || attempted.Target != txn.Target ||
   attempted.Previous != txn.Previous || !attempted.Time.Equal(txn.Time) ||
   attempted.Phase != "activating" {
   return errors.New("release attempt does not match transaction journal")
  }

  if receiptErr != nil {
   // The predecessor authority may have staged the exact target before
   // Go receipt custody existed. An audited in-flight activation may
   // requalify that immutable stage; a new activation may not.
   if err := c.requalifyAuditedStage(ctx,stage);err!=nil{
    return fmt.Errorf("audited stage requalification failed: %w",err)
   }
  }

  // A separate systemd unit owns the remaining switch. This context belongs
  // to that unit and remains alive when the executor service is replaced.
  if action == "activate" {
   var switchErr error
   // The previous executor may have been replaced after selecting the
   // target generation but before the new authority was admitted. The
   // audited guardian must prepare a current-state receipt even when the
   // NixOS system profile already names the target.
   if c.Config.BeforeSwitch!=nil {
    switchErr=c.Config.BeforeSwitch(ctx,txn.Previous,txn.Target)
    if switchErr!=nil{return c.preparationFailed(ctx,txn,switchErr)}
   }
   if switchErr == nil { switchErr=c.set(ctx,txn.Target) }
   if switchErr == nil { switchErr = c.waitForHealth(ctx) }
   if switchErr == nil {
    txn.Phase = "active"
    return c.atomic("journal.json", txn)
   }
   cleanup, cancel := context.WithTimeout(context.Background(), 90*time.Second)
   defer cancel()
   if rollbackErr := c.rollback(cleanup, txn); rollbackErr != nil {
    return fmt.Errorf("activation failed: %v; rollback failed: %w", switchErr, rollbackErr)
   }
   return fmt.Errorf("activation failed and previous generation restored: %w", switchErr)
  }
  cleanup, cancel := context.WithTimeout(context.Background(), 90*time.Second)
  defer cancel()
  return c.rollback(cleanup, txn)
 })
}


// requalifyAuditedStage is reached only after the caller has verified an
// accepted/started privileged audit record, exact in-flight journal and host.
func (c *Controller) requalifyAuditedStage(ctx context.Context, stage Manifest) error {
 if !c.Config.TrustedSource{return errors.New("trusted source required for audited requalification")}
 if err:=requireRootOwnedParents(c.Config.FlakeDir);err!=nil{return err}
 if err:=c.verifyImmutableStageBuild(ctx,stage);err!=nil{return err}
 return c.atomic("stage-ready-"+stage.ID+".json",stage)
}

// verifyImmutableStageBuild independently rebuilds the immutable Git object
// and compares its Nix store identity with the protected legacy manifest.
func (c *Controller) verifyImmutableStageBuild(ctx context.Context, stage Manifest) error {
 if stage.Host!=c.Config.Host||!revisionPattern.MatchString(stage.Revision)||!checkStorePath(stage.System){
  return errors.New("invalid legacy stage")
 }
 ref:=stage.Revision+"^{commit}"
 resolved,err:=c.Config.Run(ctx,c.Config.Git,"-C",c.Config.FlakeDir,"rev-parse","--verify",ref)
 if err!=nil||strings.TrimSpace(resolved)!=stage.Revision{return errors.New("stage source revision is unavailable")}
 source:=(&url.URL{Scheme:"git+file",Path:c.Config.FlakeDir,
  RawQuery:"rev="+stage.Revision,
  Fragment:"nixosConfigurations."+stage.Host+".config.system.build.toplevel"}).String()
 system,err:=c.Config.Run(ctx,c.Config.Nix,"build","--offline","--no-link","--print-out-paths",source)
 if err!=nil{return fmt.Errorf("immutable stage Nix build failed: %w",err)}
 if strings.TrimSpace(system)!=stage.System{
  return errors.New("immutable stage does not match rebuilt NixOS generation")
 }
 return nil
}
