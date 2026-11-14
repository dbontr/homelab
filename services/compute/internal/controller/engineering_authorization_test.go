package controller

import (
 "strings"
 "testing"
)

func TestEngineeringDraftPublicationRequiresProjectEditorAndLinkedRepository(t *testing.T) {
 db, service, endpoint := fixture(t)
 project(t, endpoint)

 const principal = "oidc:test-issuer#engineering-service"
 if _, err := db.SetProjectGrant("demo", principal, "viewer", ""); err != nil {
  t.Fatal(err)
 }
 member := caller{Principal: principal}
 args := map[string]any{"project_id":"demo","repository":"sample/repository"}
 if service.authorizeTool(member,"github_publish_artifacts",args) {
  t.Fatal("viewer may not publish draft changes")
 }
 if _, err := db.SetProjectGrant("demo", principal, "editor", "viewer"); err != nil {
  t.Fatal(err)
 }
 if !service.authorizeTool(member,"github_publish_artifacts",args) {
  t.Fatal("project editor should be allowed to prepare a draft")
 }
 if service.authorizeTool(member,"github_publish_artifacts",map[string]any{"project_id":"other-project","repository":"sample/repository"}) {
  t.Fatal("engineering service could publish into an ungranted project")
 }
 if service.authorizeTool(member,"github_merge_pull_request",args) {
  t.Fatal("engineering service must not merge pull requests")
 }
 if service.authorizeTool(member,"github_repository_api",args) {
  t.Fatal("engineering service must not make generic GitHub API mutations")
 }
 _, err := service.callToolFor(member,"github_publish_artifacts",map[string]any{
  "project_id":"demo","repository":"sample/unlinked","base_branch":"main",
  "expected_head":strings.Repeat("a",40),"changes":map[string]any{"README.md":"# Draft\n"},
  "title":"Review project artifacts",
 })
 if err==nil||!strings.Contains(err.Error(),"linked") {
  t.Fatalf("unlinked repository was not denied before using GitHub credentials: %v",err)
 }
}
