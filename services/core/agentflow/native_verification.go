package agentflow

import (
    "context"
    "errors"
)

// The engineering service owns the exact user-approved command. The native
// worker receives only the corresponding fixed tool ID and immutable commit.
func nativeVerificationAction(command string) (string, bool) {
    switch command {
    case "go test -count=1 ./...", "go test ./...":
        return "go-test", true
    case "go vet ./...":
        return "go-vet", true
    case "cargo test --locked --offline --all-targets":
        return "cargo-test", true
    case "cargo check --locked --offline":
        return "cargo-check", true
    case "cargo clippy --locked --offline --all-targets -- -D warnings":
        return "cargo-clippy", true
    default:
        return "", false
    }
}

// verificationCommand preserves existing project leases, receipts and
// artifact provenance; it does not grant the model a generic local executor.
func (e *Engine) verificationCommand(ctx context.Context, workflow Workflow, repository, command string) (map[string]any, error) {
    if !e.NativeVerification {
        return e.workspaceAction(ctx, workflow, "command",
            map[string]any{"command": "cd " + shellQuote(repoRoot(repository)) + " && " + command})
    }
    if workflow.Inputs == nil || workflow.Inputs.Primary.Repository != repository ||
        !commitName.MatchString(workflow.Inputs.Primary.Revision) {
        return nil, errors.New("native check requires a pinned primary checkout")
    }
    action, ok := nativeVerificationAction(command)
    if !ok {
        return nil, errors.New("native verification tool is not in the approved list")
    }
    return e.workspaceAction(ctx, workflow, "command", map[string]any{
        "sandbox_check": true,
        "repository": repository,
        "revision": workflow.Inputs.Primary.Revision,
        "tool": action,
    })
}
