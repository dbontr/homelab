package controller

import (
 "errors"
)

// UnlinkRepositoryExact removes only an explicitly identified, unused project
// link. It never deletes Git objects, project history, historical workspace
// source pins, or shared libraries. Revision guards prevent accidental unlink.
func (db *Database) UnlinkRepositoryExact(project, repository, expectedRole, expectedRef string) (map[string]any, error) {
 if err := requireID(project); err != nil { return nil, err }
 if !validSourceRepository(repository) || !repositoryRoles[expectedRole] ||
  len(expectedRef)>200 || (expectedRef!="" && !regexRef(expectedRef)) {
  return nil, errors.New("invalid exact repository unlink request")
 }
 var result map[string]any
 err := db.transaction(func() error {
  if err:=requireProject(db,project); err!=nil {return err}
  row,err:=db.lockedOne(`SELECT COALESCE(m.role,'source') AS role,
   COALESCE(m.default_ref,'') AS default_ref FROM repositories r
   LEFT JOIN repository_metadata m ON m.project=r.project AND m.full_name=r.full_name
   WHERE r.project=? AND r.full_name=?`,project,repository)
  if err!=nil{return err}
  if row==nil{return errors.New("repository membership not found")}
  if asString(row["role"])!=expectedRole || asString(row["default_ref"])!=expectedRef{
   return errors.New("repository membership revision conflict")
  }
  live,err:=db.lockedOne(`SELECT w.id FROM workspace_repositories wr
   JOIN workspaces w ON w.id=wr.workspace
   WHERE w.project=? AND wr.repository=? AND w.status IN ('starting','ready','stopping')
   LIMIT 1`,project,repository)
  if err!=nil{return err}
  if live!=nil{return errors.New("repository has an active workspace source")}
  if err:=db.execLocked("DELETE FROM repository_metadata WHERE project=? AND full_name=?",project,repository);err!=nil{return err}
  if err:=db.execLocked("DELETE FROM repositories WHERE project=? AND full_name=?",project,repository);err!=nil{return err}
  result=map[string]any{"project":project,"repository":repository,"previous_role":expectedRole,"previous_ref":expectedRef,"status":"unlinked"}
  return nil
 })
 return result,err
}
