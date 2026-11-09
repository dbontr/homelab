package controller

import (
 "errors"
 "io"
 "net/http"
 "net/url"
 "time"
)

// SnapshotReader pins GitLab source to a verified commit and downloads a
// tar.gz archive. Provider redirects are rejected to avoid forwarding a PAT
// to a storage origin that the operator has not approved.
func (g *GitLab) SnapshotReader(repo,ref string)(string,io.ReadCloser,error){
 prefix,err:=gitLabProjectPath(repo)
 if err!=nil{return "",nil,err}
 sha,err:=g.Commit(repo,ref)
 if err!=nil{return "",nil,err}
 if !g.configured(){return "",nil,errors.New("GitLab is not configured")}
 base,err:=url.Parse(g.API)
 if err!=nil||base.Scheme!="https"||base.Host==""||
  base.User!=nil||base.RawQuery!=""||base.Fragment!=""{
  return "",nil,errors.New("invalid GitLab API origin")
 }
 target,err:=url.Parse(g.API+prefix+"/repository/archive.tar.gz")
 if err!=nil||target.Host!=base.Host||target.Scheme!=base.Scheme{
  return "",nil,errors.New("invalid GitLab archive origin")
 }
 query:=target.Query()
 query.Set("sha",sha)
 query.Set("include_lfs_blobs","false")
 target.RawQuery=query.Encode()
 req,err:=http.NewRequest(http.MethodGet,target.String(),nil)
 if err!=nil{return "",nil,errors.New("invalid GitLab archive request")}
 req.Header.Set("PRIVATE-TOKEN",g.Token)
 req.Header.Set("User-Agent","eminence-compute")
 client:=*g.Client
 client.Timeout=5*time.Minute
 client.CheckRedirect=func(*http.Request,[]*http.Request)error{
  return errors.New("GitLab archive redirect is not approved")
 }
 response,err:=client.Do(req)
 if err!=nil{return "",nil,errors.New("GitLab archive request failed")}
 if response.StatusCode!=http.StatusOK{
  response.Body.Close()
  return "",nil,errors.New("GitLab archive is unavailable")
 }
 return sha,response.Body,nil
}
