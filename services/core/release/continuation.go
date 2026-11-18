package release

import (
 "context"
 "errors"
 "fmt"
 "time"
)

// Continue completes an independently authorized generation transition whose
// initiating executor exited during replacement. It never starts a new attempt.
func (c *Controller) Continue(ctx context.Context, action, id string) error {
 if !idPattern.MatchString(id) { return errors.New("invalid release ID") }
 if action != "activate" && action != "revert" { return errors.New("unsupported release transition") }
 return c.locked(func() error {
  stage, err := c.loadStage(id)
  if err != nil { return err }
  var ready Manifest
  if err := c.read("stage-ready-"+id+".json", &ready); err != nil { return fmt.Errorf("stage authority receipt unavailable: %w",err) }
  if ready.ID != stage.ID || ready.Host != stage.Host || ready.Revision != stage.Revision ||
   ready.System != stage.System || !ready.Created.Equal(stage.Created) {
   return errors.New("stage authority receipt differs from immutable manifest")
  }
  var attempted Journal
  if err := c.read("attempt-"+id+".json", &attempted); err != nil { return fmt.Errorf("release attempt not recorded: %w",err) }
  var txn Journal
  if err := c.read("journal.json", &txn); err != nil { return err }
  if txn.ID != id || txn.Target != stage.System || txn.Previous == txn.Target ||
   !checkStorePath(txn.Previous) || !checkStorePath(txn.Target) || txn.Time.IsZero() {
   return errors.New("release journal does not match staged generation")
  }
  if attempted.ID != txn.ID || attempted.Target != txn.Target ||
   attempted.Previous != txn.Previous || !attempted.Time.Equal(txn.Time) ||
   attempted.Phase != "activating" {
   return errors.New("release attempt does not match transaction journal")
  }
  expected := "activating"
  if action == "revert" { expected = "reverting" }
  if txn.Phase != expected { return errors.New("release is not interrupted in the required phase") }
  current, err := c.Config.Current()
  if err != nil || (current != txn.Previous && current != txn.Target) {
   return errors.New("installed generation differs from the interrupted release")
  }

  // A separate systemd unit owns the remaining switch. This context belongs
  // to that unit and remains alive when the executor service is replaced.
  if action == "activate" {
   switchErr := c.set(ctx, txn.Target)
   if switchErr == nil { switchErr = c.health(ctx) }
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
