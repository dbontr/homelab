package controller

import (
    "errors"
    "fmt"
)

// workerPausedLocked is part of the same SQLite transaction as the scheduling
// decision. A heartbeat cannot clear this administrative state.
func (db *Database) workerPausedLocked(node string) (bool, error) {
    row, err := db.lockedOne("SELECT paused FROM node_maintenance WHERE node=?", node)
    if err != nil { return false, err }
    return row != nil && asInt(row["paused"]) != 0, nil
}

func (db *Database) maintenanceStatusLocked(node string) (map[string]any, error) {
    if !validID.MatchString(node) { return nil, errors.New("invalid worker ID") }
    found, err := db.lockedOne("SELECT id,last_seen FROM nodes WHERE id=?", node)
    if err != nil { return nil, err }
    if found == nil { return nil, errors.New("worker not found") }

    row, err := db.lockedOne(
        "SELECT paused,revision,updated FROM node_maintenance WHERE node=?", node)
    if err != nil { return nil, err }
    paused, revision := false, 0
    updated := float64(0)
    if row != nil {
        paused = asInt(row["paused"]) != 0
        revision = asInt(row["revision"])
        updated = asFloat(row["updated"])
    }

    counts := map[string]float64{}
    for key, query := range map[string]string{
        "active_jobs": "SELECT count(*) n FROM jobs WHERE node=? AND status='leased'",
        "active_workspaces": "SELECT count(*) n FROM workspaces WHERE node=? AND status NOT IN ('stopped','expired')",
        "unfinished_operations": "SELECT count(*) n FROM operations WHERE node=? AND status IN ('queued','leased')",
        "hardware_leases": "SELECT count(*) n FROM hardware_leases WHERE node=? AND released IS NULL",
        "job_device_leases": "SELECT count(*) n FROM task_job_device_leases WHERE node=? AND released IS NULL",
    } {
        count, err := db.countLocked(query, node)
        if err != nil { return nil, err }
        counts[key] = count
    }
    drained := true
    for _, count := range counts {
        if count != 0 { drained = false }
    }
    return map[string]any{
        "worker_id": node, "paused": paused, "revision": revision,
        "last_seen": found["last_seen"], "online": stamp()-asFloat(found["last_seen"]) < 90,
        "updated": updated, "drained": drained,
        "active_jobs": counts["active_jobs"],
        "active_workspaces": counts["active_workspaces"],
        "unfinished_operations": counts["unfinished_operations"],
        "hardware_leases": counts["hardware_leases"],
        "job_device_leases": counts["job_device_leases"],
    }, nil
}

// NodeMaintenance reports scheduling state and *all* unfinished state owned
// by one worker; the bounded project-job history is not a drain certificate.
func (db *Database) NodeMaintenance(node string) (map[string]any, error) {
    var result map[string]any
    err := db.transaction(func() error {
        var err error
        result, err = db.maintenanceStatusLocked(node)
        return err
    })
    return result, err
}

// SetNodeMaintenance uses a persistent compare-and-swap revision. A stopped
// controller never silently clears a pause; only an administrator can resume.
func (db *Database) SetNodeMaintenance(node string, paused bool, expectedRevision int) (map[string]any, error) {
    if expectedRevision < 0 { return nil, errors.New("invalid maintenance revision") }
    var result map[string]any
    err := db.transaction(func() error {
        current, err := db.maintenanceStatusLocked(node)
        if err != nil { return err }
        revision := current["revision"].(int)
        if revision != expectedRevision {
            return fmt.Errorf("maintenance revision conflict: expected %d, current %d", expectedRevision, revision)
        }
        if current["paused"].(bool) == paused {
            return errors.New("maintenance state unchanged")
        }
        // Do not admit new workloads while cleanup or device ownership is
        // unresolved, even when an administrator requests a resume.
        if !paused && !current["drained"].(bool) {
            return errors.New("worker is not drained; maintenance cannot resume")
        }
        if !paused && !current["online"].(bool) {
            return errors.New("worker heartbeat is stale; maintenance cannot resume")
        }
        next := 0
        if paused { next = 1 }
        if revision == 0 {
            err = db.execLocked("INSERT INTO node_maintenance(node,paused,revision,updated) VALUES(?,?,1,?)", node, next, stamp())
        } else {
            err = db.execLocked("UPDATE node_maintenance SET paused=?,revision=revision+1,updated=? WHERE node=? AND revision=?", next, stamp(), node, revision)
        }
        if err != nil { return err }
        result, err = db.maintenanceStatusLocked(node)
        return err
    })
    return result, err
}
