package agentflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// TeamIntegrationRequest authorizes one integration run against the exact
// revision and base branch originally assigned to the cooperative team.
type TeamIntegrationRequest struct {
	BaseBranch   string `json:"base_branch"`
	ExpectedHead string `json:"expected_head"`
	Title        string `json:"title"`
	Body         string `json:"body,omitempty"`
}

type IntegrationArtifact struct {
	Path       string `json:"path"`
	ArtifactID string `json:"artifact_id"`
	SHA256     string `json:"sha256"`
}

// IntegrateTeam records a durable verification and draft-publication workflow.
// It does not directly execute worker commands or publish from an HTTP handler.
func (e *Engine) IntegrateTeam(id string, request TeamIntegrationRequest) (TeamSummary, error) {
	if e == nil || e.Store == nil || e.Compute == nil {
		return TeamSummary{}, errors.New("engineering service unavailable")
	}
	team, err := e.GetTeam(id)
	if err != nil {
		return TeamSummary{}, err
	}
	if !team.ReadyForReview || team.Integration != nil {
		return TeamSummary{}, errors.New("cooperative team is not available for integration")
	}
	record, err := e.Store.readTeam(id)
	if err != nil {
		return TeamSummary{}, err
	}
	if request.BaseBranch != record.BaseBranch || request.ExpectedHead != record.Revision ||
		!branchName.MatchString(request.BaseBranch) || len(request.Title) == 0 || len(request.Title) > 256 ||
		len(request.Body) > 8192 || len(record.Checks) == 0 || len(record.Checks) > 8 {
		return TeamSummary{}, errors.New("invalid integration publication scope")
	}
	ids := map[string]string{}
	for _, member := range record.Members {
		ids[member.Key] = member.RunID
	}
	manifest := make([]IntegrationArtifact, 0, len(team.Artifacts))
	for _, file := range team.Artifacts {
		runID := ids[file.Member]
		workflow, err := e.Store.Get(runID)
		if err != nil || workflow.Agent == nil || !workflow.Agent.Completed {
			return TeamSummary{}, errors.New("member verification evidence unavailable")
		}
		expected := workflow.Agent.Changed[file.Path]
		if !hexDigest(expected) {
			return TeamSummary{}, errors.New("member source digest not verified")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		info, readErr := e.Compute.Call(ctx, "artifact_info", map[string]any{
			"project_id": record.Project, "artifact_id": file.ArtifactID,
		})
		cancel()
		if readErr != nil || str(info["id"]) != file.ArtifactID ||
			str(info["project"]) != record.Project || str(info["sha256"]) != expected {
			return TeamSummary{}, errors.New("member artifact does not match verified source")
		}
		manifest = append(manifest, IntegrationArtifact{
			Path: file.Path, ArtifactID: file.ArtifactID, SHA256: expected,
		})
	}
	if len(manifest) == 0 || len(manifest) > 32 {
		return TeamSummary{}, errors.New("invalid integration artifact count")
	}
	sort.Slice(manifest, func(i, j int) bool { return manifest[i].Path < manifest[j].Path })
	runID, err := generateProgramID()
	if err != nil {
		return TeamSummary{}, err
	}
	steps := []Step{
		{ID: "provision", Kind: "provision", MaxAttempts: 1, Input: encodeStep(map[string]any{
			"image_pool_id": record.ImagePoolID, "cpu": record.CPU,
			"memory_mb": record.MemoryMB, "duration_minutes": record.DurationMinutes,
		})},
		{ID: "checkout", Kind: "checkout", MaxAttempts: 1, DependsOn: []string{"provision"},
			Input: encodeStep(map[string]any{"repository": record.Repository, "ref": record.Revision})},
	}
	importIDs := make([]string, 0, len(manifest))
	for i, artifact := range manifest {
		name := fmt.Sprintf("import-%02d", i+1)
		importIDs = append(importIDs, name)
		steps = append(steps, Step{ID: name, Kind: "integration_import",
			MaxAttempts: 1, DependsOn: []string{"checkout"}, Input: encodeStep(artifact)})
	}
	steps = append(steps,
		Step{ID: "integration-verify", Kind: "integration_verify", MaxAttempts: 1,
			DependsOn: importIDs, Input: encodeStep(map[string]any{
				"repository": record.Repository, "commands": record.Checks, "artifacts": manifest,
			})},
		Step{ID: "integration-publish", Kind: "integration_publish", MaxAttempts: 1,
			DependsOn: []string{"integration-verify"}, Input: encodeStep(map[string]any{
				"repository": record.Repository, "base_branch": request.BaseBranch,
				"expected_head": record.Revision, "title": request.Title,
				"body": request.Body, "artifacts": manifest,
			})},
		Step{ID: "cleanup", Kind: "integration_cleanup", MaxAttempts: 1,
			DependsOn: []string{"integration-publish"}, Input: encodeStep(map[string]any{})},
	)
	w := Workflow{ID: runID, Project: record.Project, Steps: steps, IntegrationTeamID: id, CreatedAt: time.Now().UTC(), Deadline: time.Now().UTC().Add(time.Duration(record.DurationMinutes)*time.Minute)}
	if err := Validate(w); err != nil {
		return TeamSummary{}, err
	}
	if err := e.Store.attachTeamIntegration(id, w); err != nil {
		return TeamSummary{}, err
	}
	return e.GetTeam(id)
}

func (s *Store) attachTeamIntegration(id string, workflow Workflow) error {
	unlock, err := s.lock()
	if err != nil {
		return err
	}
	defer unlock()
	file, err := os.Open(s.teamPath(id))
	if err != nil {
		return err
	}
	var record TeamRecord
	decodeErr := json.NewDecoder(file).Decode(&record)
	closeErr := file.Close()
	if decodeErr != nil {
		return decodeErr
	}
	if closeErr != nil {
		return closeErr
	}
	if record.ID != id || record.IntegrationRunID != "" || record.Project != workflow.Project {
		return errors.New("integration already scheduled or team identity changed")
	}
	if _, err := os.Lstat(s.filename(workflow.ID)); err == nil {
		return os.ErrExist
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	workflow.Revision = 1
	for i := range workflow.Steps {
		workflow.Steps[i].Status = Pending
	}
	if err := s.save(workflow); err != nil {
		return err
	}
	record.IntegrationRunID = workflow.ID
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(s.dir, ".team-publish-")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if err := temp.Chmod(0600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(raw); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(temp.Name(), s.teamPath(id)); err != nil {
		return err
	}
	directory, err := os.Open(s.dir)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

// TeamIntegration is a separate workflow so worker execution and external
// publication use the same durable leases as ordinary engineering runs.
func (e *Engine) integrationForTeam(id string) (*RunSummary, error) {
	record, err := e.Store.readTeam(id)
	if err != nil || record.IntegrationRunID == "" {
		return nil, err
	}
	summary, err := e.Get(record.IntegrationRunID)
	if err != nil {
		return nil, err
	}
	return &summary, nil
}

func integrationFileMap(files []IntegrationArtifact) (map[string]string, error) {
	changes := make(map[string]string, len(files))
	for _, artifact := range files {
		if !validSourcePath(artifact.Path) || sensitiveSourcePath(artifact.Path) ||
			!validID(artifact.ArtifactID) || !hexDigest(artifact.SHA256) {
			return nil, errors.New("invalid integration artifact")
		}
		if _, exists := changes[artifact.Path]; exists {
			return nil, errors.New("conflicting integration paths")
		}
		changes[artifact.Path] = artifact.ArtifactID
	}
	if len(changes) == 0 || len(changes) > 32 {
		return nil, errors.New("invalid integration change set")
	}
	return changes, nil
}

// A published draft must remain identifiable after an interrupted request.
// Failed or uncertain publication is not retried without owner reconciliation.
func (s *Store) beginExternalPublication(claim Claim) error {
	unlock, err := s.lock()
	if err != nil {
		return err
	}
	defer unlock()
	workflow, err := s.read(claim.WorkflowID)
	if err != nil {
		return err
	}
	index, err := s.activeClaim(workflow, claim)
	if err != nil {
		return err
	}
	if workflow.Steps[index].ExternalStarted {
		return errors.New("publication attempt requires owner reconciliation")
	}
	workflow.Steps[index].ExternalStarted = true
	workflow.Revision++
	return s.save(workflow)
}

func validIntegrationPublication(title, body string) error {
	if strings.TrimSpace(title) == "" || len(title) > 256 || len(body) > 8192 {
		return errors.New("invalid publication title or body")
	}
	return nil
}
