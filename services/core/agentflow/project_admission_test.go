package agentflow

import (
    "context"
    "errors"
    "testing"
)

func TestEngineeringProjectGrantRevocationIsImmediate(t *testing.T) {
    allowed := true
    calls := 0
    engine := &Engine{
        ProjectGrant: func(ctx context.Context, project string) error {
            calls++
            if ctx.Err() != nil {
                return ctx.Err()
            }
            if project == "research" && allowed {
                return nil
            }
            return errors.New("project editor grant absent")
        },
    }

    if !engine.projectAllowed("research") {
        t.Fatal("live editor-granted project denied")
    }
    if engine.projectAllowed("foreign") {
        t.Fatal("foreign project escaped current grant")
    }
    if engine.projectAllowed("../research") {
        t.Fatal("invalid project reached the grant authority")
    }
    allowed = false
    if engine.projectAllowed("research") {
        t.Fatal("revoked editor grant retained access")
    }
    if calls != 3 {
        t.Fatalf("unexpected number of exact grant checks: %d", calls)
    }

    explicit := &Engine{AllowedProjects: map[string]bool{"explicit": true}}
    if !explicit.projectAllowed("explicit") || explicit.projectAllowed("foreign") {
        t.Fatal("explicit project scope changed")
    }
    if (&Engine{}).projectAllowed("research") {
        t.Fatal("unconfigured engineering engine admitted a project")
    }
}
