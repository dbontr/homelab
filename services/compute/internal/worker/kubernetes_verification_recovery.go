package worker

import (
	"context"
	"encoding/json"
	"errors"
)

// Verification jobs are fenced before the controller can admit replacement
// work. An interrupted verifier may have executed even without an outcome.
func (k *Kubernetes) reconcileVerificationJobs(ctx context.Context) error {
	if !kubernetesName.MatchString(k.Worker) {
		return errors.New("invalid verification worker identity")
	}
	raw, err := k.checked(ctx, nil, "get", "jobs", "-l",
		"app.kubernetes.io/managed-by=project-workspace,pwm.dev/kind=verification,pwm.dev/worker="+k.Worker, "-o", "json")
	if err != nil {
		return err
	}
	var inventory map[string]any
	if err := json.Unmarshal(raw, &inventory); err != nil {
		return err
	}
	entries, ok := inventory["items"].([]any)
	if !ok || len(entries) > 128 {
		return errors.New("invalid verification job inventory")
	}
	for _, item := range entries {
		job, ok := item.(map[string]any)
		if !ok {
			return errors.New("invalid verification job entry")
		}
		meta := kubeObject(job, "metadata")
		labels := kubeObject(meta, "labels")
		name, _ := meta["name"].(string)
		workspace, _ := labels["pwm.dev/workspace"].(string)
		operation, _ := labels["pwm.dev/operation"].(string)
		if !workspaceID.MatchString(workspace) || !workspaceID.MatchString(operation) ||
			name != "pwm-verify-"+operation || !k.owns(job, workspace, "verification") {
			return errors.New("verification job identity mismatch")
		}
		if _, err := k.checked(ctx, nil, "delete", "job", name,
			"--cascade=foreground", "--wait=true", "--timeout=25s"); err != nil {
			return err
		}
	}
	return nil
}
