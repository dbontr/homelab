package release

import (
 "context"
 "errors"
 "fmt"
 "net/url"
 "os"
 "path/filepath"
 "strings"
 "time"
)

// ConsoleAdoption records an independently verified, physical-console
// observation. It is not an activation receipt or a rollback authorization.
type ConsoleAdoption struct {
 ID string `json:"id"`
 Host string `json:"host"`
 Authority string `json:"authority"`
 Revision string `json:"revision"`
 System string `json:"system"`
 PreviousJournal Journal `json:"previous_journal"`
 Time time.Time `json:"time"`
}

// ReconcileConsole is reserved for a caller that has verified an authenticated
// physical root login. The release package also independently requires its
// trusted production configuration and the exact built source revision.
func (c *Controller) ReconcileConsole(ctx context.Context, revision string, running func() (string, error)) (ConsoleAdoption, error) {
 if !c.Config.TrustedSource {
  return ConsoleAdoption{}, errors.New("trusted local release configuration required")
 }
 if err := requireRootOwnedParents(c.Config.FlakeDir); err != nil {
  return ConsoleAdoption{}, err
 }
 if err := requireRootOwnedParents(c.Config.StateDir); err != nil {
  return ConsoleAdoption{}, err
 }
 return c.reconcileVerifiedConsole(ctx, revision, running)
}

// reconcileVerifiedConsole holds the same journal lock as protected releases.
// Its caller must enforce the physical-console identity before invoking it.
func (c *Controller) reconcileVerifiedConsole(ctx context.Context, revision string, running func() (string, error)) (ConsoleAdoption, error) {
 var adoption ConsoleAdoption
 if !revisionPattern.MatchString(revision) || running == nil {
  return adoption, errors.New("exact source revision and running system probe required")
 }
 err := c.locked(func() error {
  var previous Journal
  if err := c.read("journal.json", &previous); err != nil {
   return fmt.Errorf("existing protected journal required: %w", err)
  }
  if !idPattern.MatchString(previous.ID) || !checkStorePath(previous.Target) || previous.Time.IsZero() {
   return errors.New("existing release journal is invalid")
  }
  switch previous.Phase {
  case "active", "rolled-back":
  default:
   return errors.New("unsettled release journal cannot be reconciled")
  }
  profile, err := c.Config.Current()
  if err != nil || !checkStorePath(profile) {
   return errors.New("installed system profile cannot be verified")
  }
  active, err := running()
  if err != nil || !checkStorePath(active) || active != profile {
   return errors.New("running generation and installed profile disagree")
  }
  head, err := c.Config.Run(ctx, c.Config.Git, "-C", c.Config.FlakeDir, "rev-parse", "HEAD")
  if err != nil || strings.TrimSpace(head) != revision {
   return errors.New("local source revision does not match requested commit")
  }
  dirty, err := c.Config.Run(ctx, c.Config.Git, "-C", c.Config.FlakeDir, "status", "--porcelain", "--untracked-files=all")
  if err != nil || strings.TrimSpace(dirty) != "" {
   return errors.New("release source checkout is not clean")
  }
  source := (&url.URL{Scheme:"git+file", Path:c.Config.FlakeDir,
   RawQuery:"rev="+revision,
   Fragment:"nixosConfigurations."+c.Config.Host+".config.system.build.toplevel"}).String()
  built, err := c.Config.Run(ctx, c.Config.Nix, "build", "--offline", "--no-link", "--print-out-paths", source)
  if err != nil || strings.TrimSpace(built) != profile {
   return errors.New("exact source does not reproduce the installed generation")
  }
  if err := c.health(ctx); err != nil {
   return fmt.Errorf("required generation health check failed: %w", err)
  }
  if previous.Phase == "active" && previous.Target == profile {
   return errors.New("release journal already describes the running generation")
  }
  id, err := newID()
  if err != nil { return err }
  adoption = ConsoleAdoption{ID:id, Host:c.Config.Host, Authority:"physical-root-console",
   Revision:revision, System:profile, PreviousJournal:previous, Time:time.Now().UTC()}
  recordName := "console-adoption-"+id+".json"
  if _, err := os.Lstat(filepath.Join(c.Config.StateDir,recordName)); err == nil {
   return errors.New("release adoption record identity already exists")
  } else if !errors.Is(err,os.ErrNotExist) {return err}
  // Preserve the exact prior journal in an immutable, private adoption record
  // before replacing the status pointer. Neither the prior activation nor its
  // claimed authorization is rewritten.
  if err := c.atomic(recordName, adoption); err != nil {return err}
  return c.atomic("journal.json", Journal{
   Phase:"active", ID:id, Previous:"", Target:profile, Time:adoption.Time,
  })
 })
 return adoption, err
}
