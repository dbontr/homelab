package controller

import (
	"errors"
	"io"
	"strings"
	"unicode/utf8"
)

const maxLibraryDocumentBytes = 128 << 10

func validLibraryDocument(name, body string) bool {
	if !validArtifactName.MatchString(name) ||
		!(strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".txt") || strings.HasSuffix(name, ".json")) {
		return false
	}
	return body != "" && len(body) <= maxLibraryDocumentBytes &&
		utf8.ValidString(body) && !strings.ContainsRune(body, 0)
}

// PublishLibraryDocument stores immutable text in the publisher's own library.
// A linked consumer can read the resulting object without gaining write access.
func (db *Database) PublishLibraryDocument(project, library, name, body string) (map[string]any, error) {
	if !validLibraryDocument(name, body) {
		return nil, errors.New("library document requires a valid name and UTF-8 content of at most 128 KiB")
	}
	publication, err := db.PublishedProjectLibrary(project)
	if err != nil { return nil, err }
	if asString(publication["id"]) != library {
		return nil, errors.New("the project does not publish this library")
	}
	digest := checksum([]byte(body))
	result, err := db.StoreLibraryObject(library, name, "document", digest,
		strings.NewReader(body), maxLibraryDocumentBytes)
	if err != nil { return nil, err }
	result["immutable"] = true
	return result, nil
}

func (db *Database) LibraryDocuments(project, library string) ([]map[string]any, error) {
	if !db.libraryAttached(project, library) {
		return nil, errors.New("library is not linked to project")
	}
	return db.query(`SELECT name,sha256,size,kind,created
		FROM library_objects WHERE library=? AND kind='document' ORDER BY name`, library)
}

func (db *Database) ReadLibraryDocument(project, library, name string) (map[string]any, error) {
	if !db.libraryAttached(project, library) {
		return nil, errors.New("library is not linked to project")
	}
	meta, err := db.LibraryObject(library, name)
	if err != nil { return nil, err }
	if asString(meta["kind"]) != "document" || asFloat(meta["size"]) > maxLibraryDocumentBytes {
		return nil, errors.New("library object is not a text document")
	}
	file, _, err := db.OpenLibraryObject(library, name)
	if err != nil { return nil, err }
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, maxLibraryDocumentBytes+1))
	if err != nil { return nil, err }
	if len(body) > maxLibraryDocumentBytes || !utf8.Valid(body) ||
		checksum(body) != asString(meta["sha256"]) {
		return nil, errors.New("library document failed integrity validation")
	}
	return map[string]any{"library": library, "name": name, "sha256": meta["sha256"],
		"size": len(body), "body": string(body), "immutable": true}, nil
}
