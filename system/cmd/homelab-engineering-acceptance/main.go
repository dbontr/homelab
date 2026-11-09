// Command homelab-engineering-acceptance exercises a narrowly scoped autonomous
// engineering run without enabling or deploying any Homelab services.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/dbontr/homelab-new/system/agentflow"
)

var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
var prNumberPattern = regexp.MustCompile(`^[1-9][0-9]*$`)
var workspacePattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// acceptanceConfig contains no token values. All credentials remain in files.
type acceptanceConfig struct {
	Project, Repository, BaseBranch, Revision string
	ComputeURL, ComputeTokenFile              string
	ModelURL, ModelName, ModelTokenFile       string
	ImagePoolID                               string
	Timeout                                   time.Duration
	Execute                                   bool
}

func readConfig() acceptanceConfig {
	var c acceptanceConfig
	flag.StringVar(&c.Project, "project", "homelab-platform", "Approved project ID")
	flag.StringVar(&c.Repository, "repository", "dbontr/homelab-new", "Project-linked repository")
	flag.StringVar(&c.BaseBranch, "base", "main", "Draft pull-request base branch")
	flag.StringVar(&c.Revision, "revision", os.Getenv("HOMELAB_ACCEPTANCE_REVISION"), "Pinned 40-character Git commit")
	flag.StringVar(&c.ComputeURL, "compute-url", os.Getenv("HOMELAB_COMPUTE_URL"), "Compute MCP HTTPS endpoint")
	flag.StringVar(&c.ComputeTokenFile, "compute-token-file", os.Getenv("HOMELAB_COMPUTE_TOKEN_FILE"), "Private project-editor OIDC token file")
	flag.StringVar(&c.ModelURL, "model-url", os.Getenv("HOMELAB_ENGINEERING_MODEL_URL"), "Chat-completions HTTPS endpoint")
	flag.StringVar(&c.ModelName, "model", os.Getenv("HOMELAB_ENGINEERING_MODEL_NAME"), "Decision model name")
	flag.StringVar(&c.ModelTokenFile, "model-token-file", os.Getenv("HOMELAB_ENGINEERING_MODEL_TOKEN_FILE"), "Optional private model token file")
	flag.StringVar(&c.ImagePoolID, "image", "debian-bookworm-slim", "Approved OCI image pool ID")
	flag.DurationVar(&c.Timeout, "timeout", 20*time.Minute, "Maximum acceptance duration")
	flag.BoolVar(&c.Execute, "execute", false, "Authorize one isolated run and draft publication")
	flag.Parse()
	return c
}

func (c acceptanceConfig) validate() error {
	missing := make([]string, 0, 4)
	for name, value := range map[string]string{
		"revision": c.Revision, "compute-url": c.ComputeURL,
		"compute-token-file": c.ComputeTokenFile, "model-url": c.ModelURL,
		"model": c.ModelName,
	} {
		if value == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing acceptance configuration: %s", strings.Join(missing, ", "))
	}
	if !commitPattern.MatchString(c.Revision) || !repoPattern.MatchString(c.Repository) ||
		c.Project == "" || strings.ContainsAny(c.Project, "/\\\x00\r\n") ||
		c.BaseBranch != "main" || c.Timeout < 5*time.Minute || c.Timeout > 60*time.Minute {
		return errors.New("invalid acceptance scope, revision, base branch, or duration")
	}
	if c.ImagePoolID == "" || strings.ContainsAny(c.ImagePoolID, "/\\\x00\r\n") {
		return errors.New("invalid approved image pool")
	}
	return nil
}

func tokenFromFile(filename string) (string, error) {
	if filename == "" {
		return "", nil
	}
	file, err := os.Open(filename)
	if err != nil {
		return "", errors.New("model credential file unavailable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 16384 {
		return "", errors.New("model credential file must be private and regular")
	}
	raw := make([]byte, info.Size())
	n, err := io.ReadFull(file, raw)
	if err != nil || int64(n) != info.Size() {
		return "", errors.New("cannot read model credential")
	}
	secret := strings.TrimSpace(string(raw))
	if len(secret) < 32 || strings.ContainsAny(secret, "\r\n") {
		return "", errors.New("invalid model credential")
	}
	return secret, nil
}

func randomID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func acceptanceProgram(c acceptanceConfig, id string) agentflow.Program {
	filename := "tests/engineering-acceptance/" + id + ".txt"
	marker := "homelab-engineering-acceptance-" + id
	check := fmt.Sprintf("test -f %s && grep -Fqx '%s' %s", filename, marker, filename)
	return agentflow.Program{
		Project: c.Project, Repository: c.Repository, Revision: c.Revision,
		ExpectedHead: c.Revision, BaseBranch: c.BaseBranch, Autonomous: true,
		ImagePoolID: c.ImagePoolID, CPU: 1, MemoryMB: 1024, DurationMinutes: int(c.Timeout/time.Minute) + 10,
		AllowedPaths: []string{filename}, Checks: []string{check},
		Objective: "Create only " + filename + " containing precisely this line followed by a newline: " + marker + ". " +
			"Inspect the repository when useful, make a brief plan, create the file, execute the required check, then finish. " +
			"Never modify any other file or attempt a merge or deployment.",
		Title:       "Verify isolated autonomous engineering publication",
		Description: "Draft-only acceptance of project-scoped autonomous engineering. Do not merge. No infrastructure deployment.",
	}
}

func draftURL(summary agentflow.RunSummary, revision, repository string) (string, error) {
	for _, step := range summary.Steps {
		if step.Kind != "publish" || step.Status != agentflow.Succeeded {
			continue
		}
		var result struct {
			Draft   bool   `json:"draft"`
			BaseSHA string `json:"base_sha"`
			URL     string `json:"url"`
		}
		if err := json.Unmarshal(step.Result, &result); err != nil {
			return "", errors.New("invalid publication result")
		}
		parsed, err := url.Parse(result.URL)
		if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.User != nil ||
			!prNumberPattern.MatchString(strings.TrimPrefix(parsed.Path, "/"+repository+"/pull/")) ||
			!strings.HasPrefix(parsed.Path, "/"+repository+"/pull/") ||
			parsed.RawQuery != "" || parsed.Fragment != "" || !result.Draft || result.BaseSHA != revision {
			return "", errors.New("publication did not confirm a draft against the pinned source")
		}
		return result.URL, nil
	}
	return "", errors.New("successful run did not confirm draft publication")
}

func provisionedWorkspace(summary agentflow.RunSummary) string {
	for _, step := range summary.Steps {
		if step.Kind != "provision" || step.Status != agentflow.Succeeded {
			continue
		}
		var result struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(step.Result, &result) == nil && workspacePattern.MatchString(result.ID) {
			return result.ID
		}
	}
	return ""
}

// verifyLinkedRepository ensures checkout and draft publication are restricted
// to an explicitly linked source repository before any compute allocation.
func verifyLinkedRepository(ctx context.Context, compute agentflow.ToolCaller, project, repository string) error {
	response, err := compute.Call(ctx, "repository_memberships", map[string]any{"project_id": project})
	if err != nil {
		return fmt.Errorf("repository membership unavailable: %w", err)
	}
	records, ok := response["result"].([]any)
	if !ok {
		return errors.New("invalid repository membership response")
	}
	for _, raw := range records {
		entry, ok := raw.(map[string]any)
		if !ok {
			return errors.New("invalid repository membership record")
		}
		if entry["repository"] == repository && entry["role"] == "source" {
			return nil
		}
	}
	return errors.New("repository is not linked as a project source")
}

// stopWorkspace requests deletion and verifies the resulting terminal state.
// A queued stop request is not evidence that the worker has stopped.
func stopWorkspace(ctx context.Context, compute agentflow.ToolCaller, project, id string) error {
	if _, err := compute.Call(ctx, "workspace_destroy", map[string]any{"project_id": project, "workspace_id": id}); err != nil {
		return fmt.Errorf("workspace stop request rejected: %w", err)
	}
	for {
		response, err := compute.Call(ctx, "workspace_list", map[string]any{"project_id": project})
		if err != nil {
			return fmt.Errorf("workspace status unavailable: %w", err)
		}
		records, ok := response["result"].([]any)
		if !ok {
			return errors.New("invalid workspace status listing")
		}
		present := false
		for _, raw := range records {
			entry, ok := raw.(map[string]any)
			if !ok {
				return errors.New("invalid workspace status entry")
			}
			if entry["id"] != id {
				continue
			}
			present = true
			switch entry["status"] {
			case "stopped", "destroyed":
				return nil
			case "starting", "ready", "stopping":
			default:
				return fmt.Errorf("workspace stop returned unexpected state %q", entry["status"])
			}
			break
		}
		if !present {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("workspace stop not confirmed: %w", ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}
}

func run(ctx context.Context, c acceptanceConfig) (runErr error) {
	if err := c.validate(); err != nil {
		return err
	}
	compute, err := agentflow.NewComputeClientFromFile(c.ComputeURL, c.ComputeTokenFile)
	if err != nil {
		return err
	}
	probe, stop := context.WithTimeout(ctx, 12*time.Second)
	err = compute.VerifyProjectCredential(probe)
	stop()
	if err != nil {
		return fmt.Errorf("non-administrator compute identity required: %w", err)
	}
	// Read access does not establish permission to create workspaces or drafts.
	grantCtx, stopGrant := context.WithTimeout(ctx, 12*time.Second)
	err = compute.VerifyProjectEditor(grantCtx, c.Project)
	stopGrant()
	if err != nil {
		return fmt.Errorf("project editor grant required: %w", err)
	}
	if _, err := compute.Call(ctx, "workspace_list", map[string]any{"project_id": c.Project}); err != nil {
		return fmt.Errorf("project is not readable by the service principal: %w", err)
	}
	if err := verifyLinkedRepository(ctx, compute, c.Project, c.Repository); err != nil {
		return err
	}
	token, err := tokenFromFile(c.ModelTokenFile)
	if err != nil {
		return err
	}
	author, err := agentflow.NewModelAuthor(c.ModelURL, c.ModelName, token)
	if err != nil {
		return err
	}
	id, err := randomID()
	if err != nil {
		return err
	}
	program := acceptanceProgram(c, id)
	// Confirm the real provider returns a permissible decision before allocating compute.
	modelCtx, stopModel := context.WithTimeout(ctx, 90*time.Second)
	_, err = author.Decide(modelCtx, agentflow.AgentBrief{
		Objective: program.Objective, Repository: program.Repository,
		AllowedPaths: program.AllowedPaths, Checks: program.Checks,
	}, agentflow.AgentSession{})
	stopModel()
	if err != nil {
		return fmt.Errorf("model decision compatibility check failed: %w", err)
	}
	log.Printf("preflight passed: scoped identity, project access, and model decision format")
	if !c.Execute {
		log.Printf("no resources allocated; pass -execute to submit one bounded acceptance run")
		return nil
	}
	dir, err := os.MkdirTemp("", "homelab-engineering-acceptance-")
	if err != nil {
		return err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return err
	}
	// Leave private state for crash recovery and operator examination.
	log.Printf("private acceptance state: %s", dir)
	store, err := agentflow.Open(filepath.Join(dir, "state"))
	if err != nil {
		return err
	}
	engine := &agentflow.Engine{Store: store, Compute: compute, Author: author, AllowedProjects: map[string]bool{c.Project: true}}
	runCtx, stopRun := context.WithCancel(ctx)
	defer stopRun()
	socket := filepath.Join(dir, "engineering.sock")
	serverErr := make(chan error, 1)
	go func() { serverErr <- agentflow.ServeUnix(runCtx, socket, engine) }()
	client, err := agentflow.NewClient(socket)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := client.List(); err == nil {
			break
		}
		select {
		case serveErr := <-serverErr:
			return fmt.Errorf("engineering socket startup failed: %w", serveErr)
		default:
		}
		if time.Now().After(deadline) {
			return errors.New("local engineering acceptance socket unavailable")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	submitted, err := client.Submit(program)
	if err != nil {
		return err
	}
	log.Printf("submitted isolated run %s", submitted.ID)
	// RunAvailable and the local socket share the same engine and private state.
	schedulerStopped := make(chan struct{})
	go func() {
		defer close(schedulerStopped)
		engine.Serve(runCtx)
	}()
	var final agentflow.RunSummary
	var publicationURL string
	defer func() {
		if runErr != nil {
			if w, err := store.Get(submitted.ID); err == nil {
				summary := agentflow.Summarize(w)
				if summary.Status != agentflow.Succeeded && summary.Status != agentflow.Failed && summary.Status != agentflow.Cancelled {
					if err := engine.Cancel(submitted.ID, summary.Revision); err != nil {
						log.Printf("unable to fence interrupted run %s: %v", submitted.ID, err)
					}
				}
			}
		}
		stopRun()
		select {
		case <-schedulerStopped:
		case <-time.After(10 * time.Second):
			runErr = errors.Join(runErr, errors.New("engineering scheduler did not stop before cleanup"))
		}
		if w, err := store.Get(submitted.ID); err == nil {
			final = agentflow.Summarize(w)
		}
		ws := provisionedWorkspace(final)
		if ws != "" {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			if err := stopWorkspace(cleanupCtx, compute, c.Project, ws); err != nil {
				runErr = errors.Join(runErr, fmt.Errorf("workspace %s cleanup failed: %w", ws, err))
			} else {
				log.Printf("workspace %s stopped", ws)
			}
			cancel()
		}
		if runErr == nil && publicationURL != "" {
			log.Printf("accepted: reviewable draft PR %s", publicationURL)
		}
	}()
	last := ""
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		current, err := client.Get(submitted.ID)
		if err != nil {
			return err
		}
		final = current
		key := fmt.Sprintf("%s:%d", current.Status, current.Revision)
		if key != last {
			log.Printf("run %s: status=%s, revision=%d", current.ID, current.Status, current.Revision)
			last = key
		}
		switch current.Status {
		case agentflow.Succeeded:
			link, err := draftURL(current, c.Revision, c.Repository)
			if err != nil {
				return err
			}
			publicationURL = link
			return nil
		case agentflow.Failed, agentflow.Cancelled:
			return fmt.Errorf("autonomous run %s ended with status %s", current.ID, current.Status)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("acceptance interrupted: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func main() {
	log.SetFlags(log.LUTC | log.LstdFlags)
	c := readConfig()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	deadline, stop := context.WithTimeout(ctx, c.Timeout)
	defer stop()
	if err := run(deadline, c); err != nil {
		log.Fatal(err)
	}
}