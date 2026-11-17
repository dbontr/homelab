package controller

import (
	"errors"
	"fmt"
)

// ProjectCatalog includes hidden projects for administrative inspection.
// Visibility does not alter project grants, scheduling, or historical records.
func (db *Database) ProjectCatalog() ([]map[string]any, error) {
	rows, err := db.query(`SELECT p.id,p.title,
        COALESCE(c.visible,1) AS visible,
        COALESCE(c.revision,0) AS revision,
        COALESCE(c.updated,0) AS updated
        FROM projects p
        LEFT JOIN project_catalog_visibility c ON c.project=p.id
        ORDER BY p.id`)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		row["visible"] = asInt(row["visible"]) == 1
	}
	return rows, nil
}

// SetProjectCatalogVisibility changes only a project-listing preference.
// The caller must be an authenticated administrator, supply the exact project
// name as confirmation, and match the current revision.
func (db *Database) SetProjectCatalogVisibility(project string, visible bool, expectedRevision int, confirmProjectID string) (map[string]any, error) {
	if !validID.MatchString(project) || confirmProjectID != project || expectedRevision < 0 {
		return nil, errors.New("invalid catalog operation or project confirmation")
	}
	if project == "homelab-platform" && !visible {
		return nil, errors.New("the canonical platform project must remain visible")
	}
	var result map[string]any
	err := db.transaction(func() error {
		if err := requireProject(db, project); err != nil {
			return err
		}
		row, err := db.lockedOne(
			"SELECT visible,revision FROM project_catalog_visibility WHERE project=?", project)
		if err != nil {
			return err
		}
		revision, currentVisible := 0, true
		if row != nil {
			revision = asInt(row["revision"])
			currentVisible = asInt(row["visible"]) == 1
		}
		if revision != expectedRevision {
			return fmt.Errorf("catalog revision conflict: expected %d, current %d", expectedRevision, revision)
		}
		if currentVisible == visible {
			return errors.New("catalog visibility unchanged")
		}
		flag := 0
		if visible {
			flag = 1
		}
		if revision == 0 {
			err = db.execLocked(
				"INSERT INTO project_catalog_visibility(project,visible,revision,updated) VALUES(?,?,1,?)",
				project, flag, stamp())
		} else {
			err = db.execLocked(
				"UPDATE project_catalog_visibility SET visible=?,revision=revision+1,updated=? WHERE project=? AND revision=?",
				flag, stamp(), project, revision)
		}
		if err != nil {
			return err
		}
		result = map[string]any{"project_id": project, "visible": visible, "revision": revision + 1}
		return nil
	})
	return result, err
}
