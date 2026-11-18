package privileged

import (
 "context"
 "errors"
 "path/filepath"
)

// ReleaseDispatcher admits a bounded, detached systemd service. The service
// uses the executor's protected audit record to validate its one-use intent.
type ReleaseDispatcher interface {
 Dispatch(context.Context,string,string,string) error
}

type SystemdReleaseDispatcher struct {
 SystemdRun string
 Runner string
 Run Run
}

func (d SystemdReleaseDispatcher) Dispatch(ctx context.Context,operation,requestID,releaseID string) error {
 if d.Run==nil || !trustedReleasePath(d.SystemdRun) || !trustedReleasePath(d.Runner) {
  return errors.New("detached release runner is not configured")
 }
 if len(requestID)!=32 || !lowerHexRelease(requestID) || !releasePattern.MatchString(releaseID) {
  return errors.New("invalid release dispatch identity")
 }
 action:="guard-activate"
 if operation=="infrastructure__release_revert" {
  action="guard-revert"
 }else if operation!="infrastructure__release_activate"{
  return errors.New("unsupported detached release operation")
 }
 return d.Run(ctx,d.SystemdRun,
  "--no-block","--collect",
  "--unit=homelab-release-"+requestID,
  "--property=Type=oneshot",
  "--property=TimeoutStartSec=20min",
  "--property=Restart=no",
  d.Runner,action,requestID,releaseID)
}

func trustedReleasePath(path string)bool {
 return filepath.IsAbs(path) && filepath.Clean(path)==path && path!="/"
}

func lowerHexRelease(value string)bool {
 for _,c:=range value { if (c<'0'||c>'9')&&(c<'a'||c>'f') {return false} }
 return true
}
