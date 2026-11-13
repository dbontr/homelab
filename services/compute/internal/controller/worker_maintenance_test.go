package controller

import (
    "net/http"
    "strings"
    "sync"
    "testing"
)

func TestWorkerMaintenanceFencesAllocationAndJobClaims(t *testing.T) {
    db, _, srv := fixture(t)
    project(t, srv)
    if _, err := db.RegisterNode("worker-one", kubeWorkerCaps("demo")); err != nil { t.Fatal(err) }
    if _, err := db.SubmitJob("demo", "printf retained", "debian:bookworm-slim", 1, 256); err != nil { t.Fatal(err) }

    route := "/api/nodes/worker-one/maintenance"
    if code, _ := call(t, srv, http.MethodGet, route, "", nil); code != http.StatusUnauthorized {
        t.Fatalf("unauthorized maintenance inspection returned %d", code)
    }
    if code, _ := call(t, srv, http.MethodPost, route, "", map[string]any{"paused":true,"expected_revision":0}); code != http.StatusUnauthorized {
        t.Fatalf("unauthorized maintenance request returned %d", code)
    }
    code, initial := call(t, srv, http.MethodGet, route, testOwner, nil)
    if code != 200 || initial["paused"] != false || asInt(initial["revision"]) != 0 || initial["drained"] != true {
        t.Fatalf("unexpected initial maintenance state: HTTP %d, %v", code, initial)
    }
    code, paused := call(t, srv, http.MethodPost, route, testOwner, map[string]any{"paused":true,"expected_revision":0})
    if code != 200 || paused["paused"] != true || asInt(paused["revision"]) != 1 || paused["drained"] != true {
        t.Fatalf("pause failed: HTTP %d, %v", code, paused)
    }
    code, _ = call(t, srv, http.MethodPost, route, testOwner, map[string]any{"paused":false,"expected_revision":0})
    if code != http.StatusConflict { t.Fatalf("stale resume returned %d", code) }

    var wg sync.WaitGroup
    errors := make(chan string, 32)
    for i := 0; i < 16; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            if job, err := db.ClaimJob("worker-one"); err != nil || job != nil {
                errors <- "paused worker leased a job"
            }
            if _, err := db.CreateWorkspace(WorkspaceRequest{ProjectID:"demo",Image:"debian:bookworm-slim",NodeID:"worker-one"}); err == nil {
                errors <- "paused worker allocated a workspace"
            }
        }()
    }
    wg.Wait()
    close(errors)
    for err := range errors { t.Error(err) }

    plan, err := db.PlanWorkspace(WorkspaceRequest{ProjectID:"demo",Image:"debian:bookworm-slim",NodeID:"worker-one"})
    if err != nil || asInt(plan["eligible_count"]) != 0 ||
        !strings.Contains(asString(plan["advisory"]), "snapshot") {
        t.Fatalf("maintenance planner accepted unavailable worker: %v %v", plan, err)
    }
    options, ok := plan["candidates"].([]map[string]any)
    if !ok || len(options) != 1 || options[0]["eligible"] != false {
        t.Fatalf("planner did not mark paused worker ineligible: %#v", plan)
    }

    // The maintenance state is durable; another SQLite handle must observe it.
    reopened, err := OpenDatabase(db.filename)
    if err != nil { t.Fatal(err) }
    loaded, err := reopened.NodeMaintenance("worker-one")
    reopened.Close()
    if err != nil || loaded["paused"] != true || asInt(loaded["revision"]) != 1 {
        t.Fatalf("maintenance state was not durable: %v %v", loaded, err)
    }

    code, resumed := call(t, srv, http.MethodPost, route, testOwner, map[string]any{"paused":false,"expected_revision":1})
    if code != 200 || resumed["paused"] != false || asInt(resumed["revision"]) != 2 {
        t.Fatalf("resume failed: HTTP %d %v", code, resumed)
    }
    if job, err := db.ClaimJob("worker-one"); err != nil || job == nil {
        t.Fatalf("resumed worker could not lease a queued job: %v %v", job, err)
    }
}

func TestWorkerMaintenanceDrainCountsAndExistingOperationRecovery(t *testing.T) {
    db, _, srv := fixture(t)
    project(t, srv)
    if _, err := db.RegisterNode("worker-one", kubeWorkerCaps("demo")); err != nil { t.Fatal(err) }
    workspace, err := db.CreateWorkspace(WorkspaceRequest{
        ProjectID:"demo", Image:"debian:bookworm-slim", NodeID:"worker-one", DurationMinutes:15,
    })
    if err != nil { t.Fatal(err) }
    paused, err := db.SetNodeMaintenance("worker-one", true, 0)
    if err != nil || paused["drained"] != false || asInt(paused["active_workspaces"]) != 1 ||
        asInt(paused["unfinished_operations"]) != 1 {
        t.Fatalf("active workspace was not protected: %v %v", paused, err)
    }
    if _, err := db.SetNodeMaintenance("worker-one", false, 1); err == nil {
        t.Fatal("maintenance resumed with active workspace state")
    }
    stillPaused, err := db.NodeMaintenance("worker-one")
    if err != nil || stillPaused["paused"] != true || asInt(stillPaused["revision"]) != 1 {
        t.Fatalf("unsafe resume changed the maintenance revision: %v %v", stillPaused, err)
    }
    start, err := db.ClaimOperation("worker-one")
    if err != nil || start == nil || start["action"] != "start" {
        t.Fatalf("maintenance prevented existing workspace progress: %v %v", start, err)
    }
    if _, err := db.FinishOperation("worker-one", asString(start["id"]), asString(start["lease_token"]), true, map[string]any{"status":"ready"}); err != nil {
        t.Fatal(err)
    }
    afterStart, err := db.NodeMaintenance("worker-one")
    if err != nil || afterStart["drained"] != false {
        t.Fatalf("active workspace became drained: %v %v", afterStart, err)
    }
    if _, err := db.StopWorkspace("demo", asString(workspace["id"])); err != nil { t.Fatal(err) }
    stop, err := db.ClaimOperation("worker-one")
    if err != nil || stop == nil || stop["action"] != "stop" {
        t.Fatalf("maintenance denied teardown: %v %v", stop, err)
    }
    if _, err := db.FinishOperation("worker-one", asString(stop["id"]), asString(stop["lease_token"]), true, map[string]any{"removed":true}); err != nil {
        t.Fatal(err)
    }
    drained, err := db.NodeMaintenance("worker-one")
    if err != nil || drained["paused"] != true || drained["drained"] != true {
        t.Fatalf("completed teardown did not drain worker: %v %v", drained, err)
    }
    resumed, err := db.SetNodeMaintenance("worker-one", false, 1)
    if err != nil || resumed["paused"] != false || asInt(resumed["revision"]) != 2 {
        t.Fatalf("drained worker could not resume: %v %v", resumed, err)
    }
}

func TestWorkerMaintenanceDrainWaitsForLeasedJobs(t *testing.T) {
    db, _, srv := fixture(t)
    project(t, srv)
    if _, err := db.RegisterNode("worker-one", kubeWorkerCaps("demo")); err != nil { t.Fatal(err) }
    if _, err := db.SubmitJob("demo", "printf ok", "debian:bookworm-slim", 1, 256); err != nil { t.Fatal(err) }
    leased, err := db.ClaimJob("worker-one")
    if err != nil || leased == nil { t.Fatalf("job was not leased: %v %v", leased, err) }
    state, err := db.SetNodeMaintenance("worker-one", true, 0)
    if err != nil || asInt(state["active_jobs"]) != 1 || state["drained"] != false {
        t.Fatalf("active job did not block drain: %v %v", state, err)
    }
    if _, err := db.FinishJob("worker-one", asString(leased["id"]), asString(leased["lease_token"]), "succeeded", "ok"); err != nil { t.Fatal(err) }
    state, err = db.NodeMaintenance("worker-one")
    if err != nil || state["drained"] != true || asInt(state["active_jobs"]) != 0 {
        t.Fatalf("finished job failed to drain: %v %v", state, err)
    }
}
