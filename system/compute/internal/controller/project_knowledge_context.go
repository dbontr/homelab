package controller

import (
	"errors"
	"strings"
)

// ProjectKnowledgeContext reads the selected project knowledge authority.
// Only the local policy exposes controller-held notes and contributions.
func (s *Server) ProjectKnowledgeContext(project string) (map[string]any, error) {
	policy, err := s.DB.KnowledgePolicy(project)
	if err != nil {
		return nil, err
	}
	info, err := s.DB.queryOne("SELECT id,title,created FROM projects WHERE id=?", project)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, errors.New("project not found")
	}
	repositories, err := s.DB.RepositoryMemberships(project)
	if err != nil {
		return nil, err
	}
	result := map[string]any{
		"project":          info,
		"authority":        asString(policy["mode"]),
		"project_document": nil,
		"context_document": nil,
		"latest_handoff":   nil,
		"recent_decisions": []map[string]any{},
		"repositories":     repositories,
		"note_keys":        map[string]string{"Project.md": "Project.md", "Context.md": "Context.md"},
	}
	if result["authority"] == "none" {
		return result, nil
	}
	if result["authority"] == "local" {
		context, err := s.DB.ProjectContext(project)
		if err != nil { return nil, err }
		libraries, err := s.DB.ProjectLibraries(project)
		if err != nil { return nil, err }
		context["authority"] = "local"
		context["libraries"] = libraries
		context["policy_version"] = policy["version"]
		return context, nil
	}
	if result["authority"] != "git" {
		return nil, errors.New("unsupported project knowledge authority")
	}
	repository := asString(policy["repository"])
	branch := asString(policy["branch"])
	prefix := strings.TrimRight(asString(policy["prefix"]), "/")
	var revision string
	var localName string
	var local bool
	if localName, local = localRepoName(repository); local {
		if s.LocalGit == nil {
			return nil, errors.New("local Git storage is not configured")
		}
		revision, err = s.LocalGit.Resolve(localName, branch)
	} else {
		revision, err = s.GitHub.Commit(repository, branch)
	}
	if err != nil {
		return nil, err
	}
	result["repository"] = repository
	result["branch"] = branch
	result["commit_sha"] = revision
	for _, item := range []struct{ path, key string }{
		{"Project.md", "project_document"},
		{"Context.md", "context_document"},
	} {
		path := prefix + "/" + item.path
		var file map[string]any
		if local {
			file, err = s.LocalGit.ReadAtCommit(localName, revision, path)
		} else {
			var raw any
			raw, err = s.GitHub.File(repository, path, revision)
			if err == nil {
				file = gitObj(raw)
			}
		}
		if err != nil {
			// A knowledge repository may be initialized before its canonical
			// documents exist. Remote failures other than missing files fail closed.
			if strings.Contains(err.Error(), "GitHub HTTP 404") ||
				local && strings.Contains(err.Error(), "does not exist in") {
				err = nil
				continue
			}
			return nil, err
		}
		result[item.key] = file
	}
	return result, nil
}
