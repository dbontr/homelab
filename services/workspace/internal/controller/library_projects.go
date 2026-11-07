package controller

import (
	"errors"
	"strings"
)

// A library project develops and publishes a reusable library in its own
// right. Other projects link the publication without taking ownership of the
// publishing project, its repositories, or its private knowledge.
func (db *Database) CreateLibraryProject(id, title, description, kind string) (map[string]any, error) {
	if !validID.MatchString(id) || id == personalComputeScope ||
		strings.TrimSpace(title) == "" || len(title) > 256 ||
		len(description) > 8000 || !libraryKinds[kind] {
		return nil, errors.New("invalid library project metadata")
	}
	now := stamp()
	err := db.transaction(func() error {
		if err := db.execLocked("INSERT INTO projects(id,title,created) VALUES(?,?,?)",
			id, title, now); err != nil {
			return err
		}
		if err := db.execLocked("INSERT INTO libraries(id,title,description,kind,created) VALUES(?,?,?,?,?)",
			id, title, description, kind, now); err != nil {
			return err
		}
		if err := db.execLocked("INSERT INTO project_library_publications(project,library,created) VALUES(?,?,?)",
			id, id, now); err != nil {
			return err
		}
		return db.execLocked("INSERT INTO project_libraries(project,library,linked) VALUES(?,?,?)",
			id, id, now)
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id": id, "title": title, "kind": "library",
		"published_library": id, "library_kind": kind,
	}, nil
}

// PublishedProjectLibrary resolves a project's own publication rather than
// a library it consumes. The consumer's existing library grant is independent.
func (db *Database) PublishedProjectLibrary(project string) (map[string]any, error) {
	if err := requireID(project); err != nil {
		return nil, err
	}
	record, err := db.queryOne(`SELECT l.id,l.title,l.description,l.kind,lp.project AS publishing_project
		FROM project_library_publications lp
		JOIN libraries l ON l.id=lp.library WHERE lp.project=?`, project)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, errors.New("project does not publish a library")
	}
	return record, nil
}
