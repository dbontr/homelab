package release

import (
 "context"
 "errors"
 "fmt"
 "time"
)

// FleetStageAgent builds and records a release on a protected host. It cannot
// switch the active system generation.
type FleetStageAgent interface {
 Stage(context.Context, string) (Manifest, error)
}

type FleetStageTarget struct {
 Host string `json:"host"`
 Role string `json:"role"`
}

type FleetStagePlan struct {
 ID string `json:"id,omitempty"`
 Revision string `json:"revision"`
 Targets []FleetTarget `json:"targets"`
 Manifests []Manifest `json:"manifests"`
 CreatedAt time.Time `json:"created_at"`
}

// StageFleet verifies every target before calling any agent, and requires
// the exact requested source commit in every returned host manifest.
// A partial plan on error documents completed staging, but is not executable.
func StageFleet(ctx context.Context, revision string, targets []FleetStageTarget, agents map[string]FleetStageAgent) (FleetStagePlan, error) {
 plan := FleetStagePlan{Revision: revision}
 if !revisionPattern.MatchString(revision) || len(targets) == 0 || len(targets) > 16 {
  return plan, errors.New("invalid exact source revision or fleet size")
 }
 seen := map[string]bool{}
 control := false
 for i, target := range targets {
  if !fleetHostPattern.MatchString(target.Host) || seen[target.Host] || agents[target.Host] == nil ||
   (target.Role != "compute" && target.Role != "control") {
   return plan, errors.New("invalid or unconfigured staging target")
  }
  if target.Role == "control" {
   if control || i != len(targets)-1 {
    return plan, errors.New("control host must be the final and only control target")
   }
   control = true
  }
  seen[target.Host] = true
 }
 plan.CreatedAt = time.Now().UTC()
 for _, target := range targets {
  if err := ctx.Err(); err != nil { return plan, err }
  manifest, err := agents[target.Host].Stage(ctx, revision)
  if err != nil { return plan, fmt.Errorf("stage %s: %w", target.Host, err) }
  if manifest.Host != target.Host || manifest.Revision != revision || !idPattern.MatchString(manifest.ID) ||
   !checkStorePath(manifest.System) {
   return plan, fmt.Errorf("stage %s returned a mismatched or unsafe generation", target.Host)
  }
  plan.Targets = append(plan.Targets, FleetTarget{Host:target.Host,Role:target.Role,ReleaseID:manifest.ID})
  plan.Manifests = append(plan.Manifests, manifest)
 }
 return plan, nil
}

// Validate checks a persisted plan before an owner-authorized activation.
// A plan is tied to a single exact Git revision on every host.
func (p FleetStagePlan) Validate(agents map[string]HostReleaseAgent) error {
 if !revisionPattern.MatchString(p.Revision) || len(p.Targets) != len(p.Manifests) {
  return errors.New("invalid fleet stage plan")
 }
 if err := validateFleet(p.Targets, agents); err != nil { return err }
 for i, manifest := range p.Manifests {
  if manifest.Host != p.Targets[i].Host || manifest.ID != p.Targets[i].ReleaseID ||
   manifest.Revision != p.Revision || !checkStorePath(manifest.System) {
   return errors.New("fleet manifest does not match authorized target")
  }
 }
 return nil
}
