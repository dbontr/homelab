package worker

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// Kubernetes image builds use an operator-approved rootless BuildKit image and
// an authenticated OCI registry. No Docker socket or host filesystem is exposed
// to the build Pod.
var buildDigestLine = regexp.MustCompile("(?m)^IMAGE_ID=(sha256:[a-f0-9]{64})$")
var approvedBuilderReference = regexp.MustCompile("^[a-z0-9][a-z0-9./:_-]+@sha256:[a-f0-9]{64}$")
var approvedRegistryPrefix = regexp.MustCompile("^[a-z0-9][a-z0-9.-]*(?::[0-9]{2,5})?(?:/[a-z0-9][a-z0-9._-]*){0,3}$")

func validBuilderImage(image string) bool {
	return len(image) <= 240 && approvedBuilderReference.MatchString(image) &&
		!strings.Contains(image, "://") && !strings.Contains(image, "//") &&
		!strings.Contains(image, "..")
}

func validBuilderRegistry(prefix string) bool {
	return len(prefix) <= 128 && approvedRegistryPrefix.MatchString(prefix) &&
		!strings.Contains(prefix, "..") && strings.ContainsAny(strings.Split(prefix, "/")[0], ".:")
}


// Only controller-owned project environment names can extend the ordinary worker
// image allowlist. The controller verifies that an environment is ready.
func (k *Kubernetes) approvedRuntimeImage(r *Runner, image string) bool {
	return r.Images[image] || (k.BuilderRegistry != "" &&
		imageName.MatchString(image) &&
		strings.HasPrefix(image, k.BuilderRegistry+"/pwm-env-"+k.Project+"-"))
}

func (k *Kubernetes) buildManifest(op Operation, image, workdir string) map[string]any {
	labels := k.labels(op.Workspace, "image-builder")
	name := "pwm-build-" + op.ID
	command := "set -eu; umask 077; export HOME=/home/user; export DOCKER_CONFIG=/home/user/.docker; " +
		"buildctl-daemonless.sh build --frontend dockerfile.v0 " +
		"--local context=" + shellQuote(workdir) + " --local dockerfile=" + shellQuote(workdir) +
		" --output " + shellQuote("type=image,name="+image+",push=true") +
		" --metadata-file /tmp/pwm-image-metadata.json; " +
		"sed -n 's/.*\"containerimage.digest\": *\"\\(sha256:[a-f0-9]\\{64\\}\\)\".*/IMAGE_ID=\\1/p' /tmp/pwm-image-metadata.json"

	podSpec := map[string]any{
		"restartPolicy": "Never", "automountServiceAccountToken": false,
		"serviceAccountName": "pwm-sandbox",
		"securityContext": map[string]any{
			"runAsUser": 1000, "runAsGroup": 1000,
			"fsGroup": 65534, "runAsNonRoot": true,
			"seccompProfile": map[string]string{"type": "RuntimeDefault"},
		},
		"containers": []any{map[string]any{
			"name": "builder", "image": k.BuilderImage,
			"command": []string{"/bin/sh", "-c", command},
			"env": []any{
				map[string]string{"name": "BUILDKITD_FLAGS", "value": "--oci-worker-no-process-sandbox --oci-worker-snapshotter=native"},
				map[string]string{"name": "HOME", "value": "/home/user"},
			},
			"securityContext": map[string]any{
				"allowPrivilegeEscalation": false, "runAsNonRoot": true,
				"runAsUser": 1000, "runAsGroup": 1000,
				"capabilities": map[string]any{"drop": []string{"ALL"}},
				"seccompProfile": map[string]string{"type": "RuntimeDefault"},
			},
			"resources": kubernetesResources(1, 2048, nil),
			"volumeMounts": []any{
				map[string]any{"name": "source", "mountPath": "/workspace", "readOnly": true},
				map[string]any{"name": "scratch", "mountPath": "/tmp"},
				map[string]any{"name": "registry", "mountPath": "/home/user/.docker", "readOnly": true},
			},
		}},
		"volumes": []any{
			map[string]any{"name": "source", "persistentVolumeClaim": map[string]string{"claimName": kubeClaimName(op.Workspace)}},
			map[string]any{"name": "scratch", "emptyDir": map[string]string{"sizeLimit": "4Gi"}},
			map[string]any{"name": "registry", "secret": map[string]any{"secretName": k.BuilderAuthSecret, "items": []any{
				map[string]string{"key": ".dockerconfigjson", "path": "config.json"},
			}}},
		},
	}
	if k.WorkloadNode != "" {
		podSpec["nodeSelector"] = map[string]string{"kubernetes.io/hostname": k.WorkloadNode}
	}
	if k.ImagePullSecret != "" {
		podSpec["imagePullSecrets"] = []any{map[string]string{"name": k.ImagePullSecret}}
	}
	return map[string]any{
		"apiVersion": "batch/v1", "kind": "Job",
		"metadata": map[string]any{"name": name, "labels": labels},
		"spec": map[string]any{
			"backoffLimit": 0, "activeDeadlineSeconds": 3600, "ttlSecondsAfterFinished": 600,
			"template": map[string]any{"metadata": map[string]any{"labels": labels}, "spec": podSpec},
		},
	}
}

func approvedBuildDockerfile(base, content string) bool {
	if content == "" {
		return true
	}
	first := ""
	fromCount := 0
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if first == "" {
			first = line
		}
		if len(fields) > 0 && strings.EqualFold(fields[0], "FROM") {
			fromCount++
		}
	}
	return fromCount == 1 && first == "FROM "+base
}

func (k *Kubernetes) BuildImage(ctx context.Context, r *Runner, op Operation, p map[string]any) (map[string]any, error) {
	if k.BuilderImage == "" || k.BuilderRegistry == "" || k.BuilderAuthSecret == "" || k.WorkloadNode == "" {
		return nil, errors.New("Kubernetes image building requires an approved registry, builder image, registry Secret, and pinned workload node")
	}
	if !workspaceID.MatchString(op.ID) || !workspaceID.MatchString(op.Workspace) {
		return nil, errors.New("invalid image-build operation identity")
	}
	image, base := getString(p, "image"), getString(p, "base_image")
	if !imageName.MatchString(image) || !strings.HasPrefix(image, k.BuilderRegistry+"/") ||
		!r.Images[base] || !approvedBuildDockerfile(base, getString(p, "dockerfile")) {
		return nil, errors.New("unapproved environment image, base, or Dockerfile")
	}
	if p["context"] != nil && getString(p, "context") != "" {
		return nil, errors.New("Kubernetes image building does not support external named build contexts")
	}
	if architecture := getString(p, "architecture"); architecture != "" && architecture != runtime.GOARCH {
		return nil, errors.New("environment architecture does not match the worker")
	}
	if err := k.requireWorkspace(ctx, op.Workspace); err != nil {
		return nil, err
	}
	dockerfile := getString(p, "dockerfile")
	if len(dockerfile) > 16384 {
		return nil, errors.New("Dockerfile exceeds size limit")
	}
	if dockerfile == "" {
		dockerfile = "FROM " + base + "\n"
	}
	dir := "/workspace/.pwm-build/" + op.ID
	write := "set -eu; umask 022; mkdir -p " + shellQuote(dir) + "; cat > " + shellQuote(dir+"/Dockerfile")
	if _, err := r.workspaceExec(ctx, 2*time.Minute, strings.NewReader(dockerfile), op.Workspace, "sh", "-c", write); err != nil {
		return nil, fmt.Errorf("stage Dockerfile on workspace volume: %w", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = r.workspaceExec(cleanup, 20*time.Second, nil, op.Workspace,
			"rm", "-rf", "--", dir)
	}()

	name := "pwm-build-" + op.ID
	if err := k.create(ctx, k.buildManifest(op, image, dir)); err != nil {
		return nil, fmt.Errorf("create Kubernetes builder Job: %w", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = k.checked(cleanup, nil, "delete", "job", name,
			"--cascade=foreground", "--wait=false")
	}()
	run, cancel := context.WithTimeout(ctx, 65*time.Minute)
	defer cancel()
	_, stderr, status, err := k.call(run, 64*1024, nil,
		"wait", "--for=condition=complete", "job/"+name, "--timeout=3600s")
	if err != nil || status != 0 {
		logs, _ := k.checked(ctx, nil, "logs", "job/"+name, "--all-containers=true", "--tail=40")
		return nil, fmt.Errorf("builder did not complete: %v %s: %s", err,
			strings.TrimSpace(string(stderr)), strings.TrimSpace(string(logs)))
	}
	logs, err := k.checked(run, nil, "logs", "job/"+name, "--all-containers=true")
	if err != nil {
		return nil, fmt.Errorf("inspect completed image build: %w", err)
	}
	digests := buildDigestLine.FindAllSubmatch(logs, -1)
	if len(digests) != 1 {
		return nil, errors.New("Kubernetes builder did not report exactly one verified OCI digest")
	}
	return map[string]any{"image": image, "image_id": string(digests[0][1])}, nil
}

// A published image is registry-backed; Docker daemon image archives do not
// apply to the Kubernetes worker's runtime.
func (k *Kubernetes) RegistryImageUnavailable(action string) (map[string]any, error) {
	return nil, fmt.Errorf("%s is unsupported for registry-backed Kubernetes images; use the immutable OCI image reference", action)
}
