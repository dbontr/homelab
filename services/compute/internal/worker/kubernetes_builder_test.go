package worker

import (
	"strings"
	"testing"
)

func TestKubernetesBuilderConfigurationIsExplicitAndPinned(t *testing.T) {
	digest := strings.Repeat("a", 64)
	for _, image := range []string{
		"", "moby/buildkit:rootless", "ghcr.io/example/buildkit:latest",
		"moby/buildkit@sha256:not-a-digest", "http://registry/buildkit@sha256:" + digest,
	} {
		if validBuilderImage(image) {
			t.Fatalf("builder image must be digest-pinned: %q", image)
		}
	}
	if !validBuilderImage("docker.io/moby/buildkit:rootless@sha256:" + digest) {
		t.Fatal("a digest-pinned BuildKit OCI image was rejected")
	}
	for _, prefix := range []string{
		"", "http://registry.example", "localhost/../repo", "registry..example/project",
		"registry.example/project/", "registry.example/project?token=x",
	} {
		if validBuilderRegistry(prefix) {
			t.Fatalf("invalid registry prefix accepted: %q", prefix)
		}
	}
	for _, prefix := range []string{"ghcr.io/dbontr", "registry.example:443/workspace"} {
		if !validBuilderRegistry(prefix) {
			t.Fatalf("valid registry prefix rejected: %q", prefix)
		}
	}
}

func TestBuilderDockerfileMustUseOneApprovedBase(t *testing.T) {
	if !approvedBuildDockerfile("golang:1.27.3-bookworm", "") ||
		!approvedBuildDockerfile("golang:1.27.3-bookworm",
			"FROM golang:1.27.3-bookworm\nRUN go version\n") {
		t.Fatal("approved single-stage Dockerfile rejected")
	}
	for _, file := range []string{
		"FROM rust:1.99.1-bookworm\n",
		"FROM golang:1.27.3-bookworm\nFROM rust:1.99.1-bookworm\n",
		"ARG TARGET=golang:1.27.3-bookworm\nFROM ${TARGET}\n",
		"RUN true\nFROM golang:1.27.3-bookworm\n",
	} {
		if approvedBuildDockerfile("golang:1.27.3-bookworm", file) {
			t.Fatalf("unapproved build input accepted: %q", file)
		}
	}
}

func TestBuilderPodIsNamespaceScopedWithoutHostPrivileges(t *testing.T) {
	k := &Kubernetes{
		Namespace: "pwm-research", Project: "research", Worker: "research-agent",
		BuilderImage: "docker.io/moby/buildkit:rootless@sha256:" + strings.Repeat("a", 64),
		BuilderRegistry: "ghcr.io/dbontr",
		BuilderAuthSecret: "registry-credentials",
		WorkloadNode: "mercury", ImagePullSecret: "registry-pull",
	}
	op := Operation{ID: strings.Repeat("b", 32), Workspace: strings.Repeat("c", 32)}
	image := "ghcr.io/dbontr/pwm-env-research-toolchain:v1"
	job := k.buildManifest(op, image, "/workspace/.pwm-build/"+op.ID)
	if job["kind"] != "Job" {
		t.Fatal("builder is not an isolated batch Job")
	}
	spec := kubeObject(job, "spec")
	template := kubeObject(spec, "template")
	pod := kubeObject(template, "spec")
	if pod["automountServiceAccountToken"] != false ||
		pod["serviceAccountName"] != "pwm-sandbox" {
		t.Fatal("builder received service-account credentials")
	}
	if selector, _ := pod["nodeSelector"].(map[string]string); selector["kubernetes.io/hostname"] != "mercury" {
		t.Fatal("builder escaped its assigned worker node")
	}
	containers, ok := pod["containers"].([]any)
	if !ok || len(containers) != 1 { t.Fatal("unexpected builder containers") }
	builder := containers[0].(map[string]any)
	if builder["image"] != k.BuilderImage { t.Fatal("wrong builder image") }
	security := kubeObject(builder, "securityContext")
	if security["runAsNonRoot"] != true || security["allowPrivilegeEscalation"] != false {
		t.Fatal("builder requires nonroot execution without privilege escalation")
	}
	volumes := pod["volumes"].([]any)
	if len(volumes) != 3 { t.Fatal("unexpected builder volumes") }
	claim, ok := volumes[0].(map[string]any)["persistentVolumeClaim"].(map[string]string)
	if !ok || claim["claimName"] != kubeClaimName(op.Workspace) {
		t.Fatal("builder mount is not the project's workspace claim")
	}
	registry := kubeObject(volumes[2].(map[string]any), "secret")
	if registry["secretName"] != k.BuilderAuthSecret {
		t.Fatal("registry authentication is not supplied by a preprovisioned namespace Secret")
	}
	if !imageName.MatchString(image) || imageName.MatchString("other/unsafe:latest") {
		t.Fatal("registry-qualified environment image name validation failed")
	}
	allowed := &Runner{Images: map[string]bool{"debian:bookworm-slim": true}}
	if !k.approvedRuntimeImage(allowed, image) ||
		!k.approvedRuntimeImage(allowed, "debian:bookworm-slim") ||
		k.approvedRuntimeImage(allowed, "ghcr.io/dbontr/pwm-env-other-toolchain:v1") ||
		k.approvedRuntimeImage(allowed, "ghcr.io/untrusted/pwm-env-research-toolchain:v1") {
		t.Fatal("runtime image approval violated project and registry boundaries")
	}

}
