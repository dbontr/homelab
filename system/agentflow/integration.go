package agentflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"time"
)

func (e *Engine) integrationStep(ctx context.Context, claim Claim, workflow Workflow, args map[string]any) (map[string]any, error) {
	project := workflow.Project
	switch claim.Kind {
	case "integration_import":
		var artifact IntegrationArtifact
		if err := json.Unmarshal(claim.Input, &artifact); err != nil {
			return nil, err
		}
		if !validSourcePath(artifact.Path) || sensitiveSourcePath(artifact.Path) ||
			!validID(artifact.ArtifactID) || !hexDigest(artifact.SHA256) {
			return nil, errors.New("invalid integration source")
		}
		info, err := e.Compute.Call(ctx, "artifact_info", map[string]any{
			"project_id": project, "artifact_id": artifact.ArtifactID,
		})
		if err != nil {
			return nil, err
		}
		if str(info["id"]) != artifact.ArtifactID || str(info["project"]) != project ||
			str(info["sha256"]) != artifact.SHA256 {
			return nil, errors.New("integration artifact digest or owner differs")
		}
		checkout, err := resultObject(workflow, "checkout")
		if err != nil {
			return nil, err
		}
		ws, err := workspaceID(workflow)
		if err != nil {
			return nil, err
		}
		if err := e.awaitReady(ctx, project, ws); err != nil {
			return nil, err
		}
		target := path.Join(repoRoot(str(checkout["repository"])), artifact.Path)
		reply, err := e.Compute.Call(ctx, "workspace_import_artifact", map[string]any{
			"project_id": project, "workspace_id": ws, "artifact_id": artifact.ArtifactID, "path": target,
		})
		if err != nil {
			return nil, err
		}
		if _, err := e.awaitOperation(ctx, project, ws, str(reply["operation_id"])); err != nil {
			return nil, err
		}
		read, err := e.workspaceAction(ctx, workflow, "read_file", map[string]any{"path": target})
		if err != nil {
			return nil, err
		}
		actual := sha256.Sum256([]byte(str(read["content"])))
		if hex.EncodeToString(actual[:]) != artifact.SHA256 {
			return nil, errors.New("imported artifact differs from verified source")
		}
		return map[string]any{"path": artifact.Path, "sha256": artifact.SHA256, "imported": true}, nil
	case "integration_verify":
		var input struct {
			Repository string                `json:"repository"`
			Commands   []string              `json:"commands"`
			Artifacts  []IntegrationArtifact `json:"artifacts"`
		}
		if err := json.Unmarshal(claim.Input, &input); err != nil {
			return nil, err
		}
		if !repositoryName.MatchString(input.Repository) || len(input.Commands) == 0 ||
			len(input.Commands) > 8 || len(input.Artifacts) == 0 {
			return nil, errors.New("invalid integrated verification input")
		}
		verified := make(map[string]string, len(input.Artifacts))
		for _, file := range input.Artifacts {
			if !validSourcePath(file.Path) || sensitiveSourcePath(file.Path) || !hexDigest(file.SHA256) {
				return nil, errors.New("untrusted integration file")
			}
			verified[file.Path] = file.SHA256
		}
		results := make([]map[string]any, 0, len(input.Commands))
		for _, command := range input.Commands {
			if len(command) == 0 || len(command) > 512 {
				return nil, errors.New("verification command exceeds bounds")
			}
			result, err := e.workspaceAction(ctx, workflow, "command", map[string]any{
				"command": "cd " + shellQuote(repoRoot(input.Repository)) + " && " + command,
			})
			if err != nil {
				return nil, err
			}
			exit, ok := result["exit_code"].(float64)
			if !ok || exit != 0 {
				return nil, errors.New("combined source failed an owner-approved check")
			}
			results = append(results, map[string]any{"command": command, "exit_code": 0})
		}
		for name, expected := range verified {
			read, err := e.workspaceAction(ctx, workflow, "read_file", map[string]any{
				"path": path.Join(repoRoot(input.Repository), name),
			})
			if err != nil {
				return nil, err
			}
			digest := sha256.Sum256([]byte(str(read["content"])))
			if hex.EncodeToString(digest[:]) != expected {
				return nil, errors.New("combined verification altered an imported source")
			}
		}
		return map[string]any{"passed": true, "checks": results, "file_sha256": verified}, nil
	case "integration_publish":
		verified, err := resultObject(workflow, "integration-verify")
		if err != nil || verified["passed"] != true {
			return nil, errors.New("combined verification is not confirmed")
		}
		var input struct {
			Repository   string                `json:"repository"`
			BaseBranch   string                `json:"base_branch"`
			ExpectedHead string                `json:"expected_head"`
			Title        string                `json:"title"`
			Body         string                `json:"body"`
			Artifacts    []IntegrationArtifact `json:"artifacts"`
		}
		if err := json.Unmarshal(claim.Input, &input); err != nil {
			return nil, err
		}
		if !repositoryName.MatchString(input.Repository) || !branchName.MatchString(input.BaseBranch) ||
			!commitName.MatchString(input.ExpectedHead) {
			return nil, errors.New("invalid integrated publication target")
		}
		if err := validIntegrationPublication(input.Title, input.Body); err != nil {
			return nil, err
		}
		checkout, err := resultObject(workflow, "checkout")
		if err != nil || str(checkout["repository"]) != input.Repository ||
			str(checkout["revision"]) != input.ExpectedHead {
			return nil, errors.New("integration checkout is not pinned to publication base")
		}
		changes, err := integrationFileMap(input.Artifacts)
		if err != nil {
			return nil, err
		}
		for _, file := range input.Artifacts {
			read, err := e.workspaceAction(ctx, workflow, "read_file", map[string]any{
				"path": path.Join(repoRoot(input.Repository), file.Path),
			})
			if err != nil {
				return nil, err
			}
			sum := sha256.Sum256([]byte(str(read["content"])))
			if hex.EncodeToString(sum[:]) != file.SHA256 {
				return nil, errors.New("source changed after combined verification")
			}
			info, err := e.Compute.Call(ctx, "artifact_info", map[string]any{
				"project_id": project, "artifact_id": file.ArtifactID,
			})
			if err != nil {
				return nil, err
			}
			if str(info["sha256"]) != file.SHA256 || str(info["project"]) != project {
				return nil, errors.New("publication artifact lost its verified identity")
			}
		}
		// The intent is durable before calling GitHub. If the response is
		// interrupted, owner reconciliation is required; do not make a second PR.
		if err := e.Store.beginExternalPublication(claim); err != nil {
			return nil, err
		}
		result, err := e.Compute.Call(ctx, "github_publish_artifacts", map[string]any{
			"project_id": project, "repository": input.Repository,
			"base_branch": input.BaseBranch, "expected_head": input.ExpectedHead,
			"changes": changes, "title": input.Title, "body": input.Body,
		})
		if err != nil {
			return nil, err
		}
		if result["draft"] != true || str(result["base_sha"]) != input.ExpectedHead ||
			str(result["url"]) == "" {
			return nil, errors.New("combined draft publication was not confirmed")
		}
		return result, nil
	case "integration_cleanup":
		ws, err := workspaceID(workflow)
		if err != nil {
			return nil, err
		}
		if _, err := e.Compute.Call(ctx, "workspace_destroy", map[string]any{
			"project_id": project, "workspace_id": ws,
		}); err != nil {
			return nil, err
		}
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			list, err := e.Compute.Call(ctx, "workspace_list", map[string]any{"project_id": project})
			if err != nil {
				return nil, err
			}
			records, ok := list["result"].([]any)
			if !ok {
				return nil, errors.New("workspace listing unavailable during cleanup")
			}
			found := false
			for _, item := range records {
				entry := obj(item)
				if str(entry["id"]) != ws {
					continue
				}
				found = true
				switch str(entry["status"]) {
				case "stopped", "destroyed":
					return map[string]any{"workspace_stopped": true}, nil
				case "stopping", "ready", "starting":
				default:
					return nil, errors.New("unexpected workspace state during cleanup")
				}
			}
			if !found {
				return map[string]any{"workspace_stopped": true}, nil
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-ticker.C:
			}
		}
	default:
		return nil, fmt.Errorf("invalid integration operation %q", claim.Kind)
	}
}
