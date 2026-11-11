package worker

import (
    "context"
    "errors"
    "fmt"
    "regexp"
    "strings"
    "time"
)

var verificationRepository = regexp.MustCompile("^[A-Za-z0-9_.-]{1,100}/[A-Za-z0-9_.-]{1,100}$")
var verificationRevision = regexp.MustCompile("^[a-f0-9]{40}([a-f0-9]{24})?$")

func validVerification(repository, revision, action string) bool {
    if !verificationRepository.MatchString(repository) || strings.Contains(repository, "..") ||
        strings.Contains(repository, "/.") || strings.HasPrefix(repository, ".") ||
        !verificationRevision.MatchString(revision) {
        return false
    }
    switch action {
    case "go-test", "go-vet", "cargo-test", "cargo-check", "cargo-clippy":
        return true
    }
    return false
}

func deniesNetwork(policy map[string]any) bool {
    spec := kubeObject(policy, "spec")
    selection, ok := spec["podSelector"].(map[string]any)
    if !ok || len(selection) > 0 { return false }
    kinds, ok := spec["policyTypes"].([]any)
    if !ok { return false }
    ingress, egress := false, false
    for _, raw := range kinds {
        if raw == "Ingress" { ingress = true }
        if raw == "Egress" { egress = true }
    }
    if rules, exists := spec["ingress"]; exists {
        list, ok := rules.([]any)
        if !ok || len(list) > 0 { return false }
    }
    if rules, exists := spec["egress"]; exists {
        list, ok := rules.([]any)
        if !ok || len(list) > 0 { return false }
    }
    return ingress && egress
}

func (k *Kubernetes) verificationManifest(op Operation, repository, revision, action, node string) map[string]any {
    labels := k.labels(op.Workspace, "verification")
    labels["pwm.dev/operation"] = op.ID
    pod := map[string]any{
        "restartPolicy": "Never", "automountServiceAccountToken": false,
        "serviceAccountName": "pwm-sandbox", "nodeName": node,
        "securityContext": sandboxPodSecurity(),
        "containers": []any{map[string]any{
            "name": "verifier", "image": k.VerificationImage, "imagePullPolicy": "IfNotPresent",
            "command": []string{"/usr/local/bin/homelab-engineering-sandbox",
                "run-contained", repository, revision, action},
            "workingDir": "/workspace", "resources": kubernetesResources(1, 2048, nil),
            "securityContext": restrictedSecurity(),
            "volumeMounts": []any{
                map[string]any{"name": "source", "mountPath": "/workspace", "readOnly": true},
                map[string]any{"name": "scratch", "mountPath": "/tmp"},
            },
        }},
        "volumes": []any{
            map[string]any{"name": "source", "persistentVolumeClaim": map[string]any{
                "claimName": kubeClaimName(op.Workspace), "readOnly": true,
            }},
            map[string]any{"name": "scratch", "emptyDir": map[string]any{"sizeLimit": "4Gi"}},
        },
    }
    if k.ImagePullSecret != "" {
        pod["imagePullSecrets"] = []any{map[string]string{"name": k.ImagePullSecret}}
    }
    return map[string]any{
        "apiVersion": "batch/v1", "kind": "Job",
        "metadata": map[string]any{"name": "pwm-verify-"+op.ID, "labels": labels},
        "spec": map[string]any{
            "backoffLimit": 0, "activeDeadlineSeconds": 630, "ttlSecondsAfterFinished": 600,
            "template": map[string]any{"metadata": map[string]any{"labels": labels}, "spec": pod},
        },
    }
}

// A separate read-only Job executes only fixed, revision-bound build/test tools.
// The worker holds the project lease; no credential or host device is mounted.
func (k *Kubernetes) VerifyNativeCheck(ctx context.Context, op Operation, data map[string]any) (output map[string]any, resultErr error) {
    if !k.verificationReady() {
        return nil, errors.New("worker quarantined pending verification Job cleanup")
    }
    if len(data) != 4 || data["sandbox_check"] != true {
        return nil, errors.New("native verification requires an exact request")
    }
    repository, revision, action := getString(data, "repository"), getString(data, "revision"), getString(data, "tool")
    if !workspaceID.MatchString(op.ID) || !workspaceID.MatchString(op.Workspace) ||
        !validVerification(repository, revision, action) {
        return nil, errors.New("native verification scope is invalid")
    }
    if !validBuilderImage(k.VerificationImage) {
        return nil, errors.New("native verification image must be pinned by digest")
    }
    policy, err := k.get(ctx, "networkpolicy", "pwm-isolate-workloads")
    if err != nil || !deniesNetwork(policy) {
        return nil, errors.New("native verification requires deny-all namespace network policy")
    }
    workspace, err := k.inspectWorkspace(ctx, op.Workspace)
    if err != nil { return nil, err }
    node, _ := kubeObject(workspace, "spec")["nodeName"].(string)
    if !kubernetesName.MatchString(node) || (k.WorkloadNode != "" && node != k.WorkloadNode) {
        return nil, errors.New("verification must use the workspace volume node")
    }
    name := "pwm-verify-" + op.ID
    if err := k.ensureOwnedCreate(ctx, k.verificationManifest(op, repository, revision, action, node),
        "job", name, op.Workspace, "verification"); err != nil {
        return nil, fmt.Errorf("create constrained verification Job: %w", err)
    }
    defer func() {
        // A timed-out or cancelled request must not release its worker lease
        // while a verification Job might still be executing.
        cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()
        if _, cleanupErr := k.checked(cleanup, nil, "delete", "job", name,
            "--cascade=foreground", "--wait=true", "--timeout=25s",
            "--ignore-not-found=true"); cleanupErr != nil {
            k.verificationMu.Lock()
            k.verificationQuarantined = true
            k.verificationMu.Unlock()
            output = nil
            resultErr = errors.Join(resultErr,
                fmt.Errorf("native verification job cleanup is unconfirmed: %w", cleanupErr))
        }
    }()
    deadline, cancel := context.WithTimeout(ctx, 11*time.Minute)
    defer cancel()
    for {
        job, err := k.get(deadline, "job", name)
        if err != nil { return nil, fmt.Errorf("native verification outcome unknown: %w", err) }
        status := kubeObject(job, "status")
        if count, _ := status["succeeded"].(float64); count == 1 {
            logs, err := k.checked(deadline, nil, "logs", "job/"+name, "--all-containers=true")
            if err != nil || strings.TrimSpace(string(logs)) != "completed" {
                return nil, errors.New("native verifier did not return an exact completion receipt")
            }
            return map[string]any{"exit_code": float64(0), "stdout": "completed", "stderr": ""}, nil
        }
        if count, _ := status["failed"].(float64); count > 0 {
            logs, err := k.checked(deadline, nil, "logs", "job/"+name, "--all-containers=true", "--tail=20")
            if err != nil { return nil, errors.New("native verifier failed without a result") }
            detail := string(logs)
            if len(detail) > 4096 { detail = detail[len(detail)-4096:] }
            return map[string]any{"exit_code": float64(1), "stdout": "", "stderr": detail}, nil
        }
        select {
        case <-deadline.Done():
            return nil, fmt.Errorf("native verification outcome unknown: %w", deadline.Err())
        case <-time.After(2 * time.Second):
        }
    }
}
