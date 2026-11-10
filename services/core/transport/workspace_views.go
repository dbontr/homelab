package transport

import (
 "encoding/json"
 "net/http"
 "regexp"
 "strings"
)

var consoleProjectID=regexp.MustCompile("^[a-z][a-z0-9-]{0,79}$")

// Browser inspection never obtains a worker token, executes a command, or
// mutates project state. All operations go through the compute controller.
func (s *Server) uiWorkspaceProjectView(w http.ResponseWriter,r *http.Request){
 project:=r.PathValue("project")
 if !consoleProjectID.MatchString(project){writeError(w,400,"invalid project identifier");return}
 parts:=strings.Split(r.URL.Path,"/")
 kind:=parts[len(parts)-1]
 tool:=""
 switch kind{
 case "jobs":tool="list_project_jobs"
 case "workspaces":tool="workspace_list"
 case "artifacts":tool="list_project_artifacts"
 case "context":tool="knowledge_context"
 case "repositories":tool="repository_memberships"
 case "research-sources":tool="research_sources"
 case "research":tool="research_search"
 default:writeError(w,404,"unknown project view");return
 }
 params:=map[string]any{"project_id":project}
 if kind=="research" {
  query:=strings.TrimSpace(r.URL.Query().Get("q"))
  if len(query)==0||len(query)>120||strings.ContainsAny(query,"\x00\r\n"){writeError(w,400,"invalid research query");return}
  params["query"]=query
  params["limit"]=8
 }
 raw,err:=json.Marshal(params)
 if err!=nil{writeError(w,500,"invalid project lookup");return}
 result:=s.invoke(r.Context(),"workspace__"+tool,raw,json.RawMessage("1"))
 if failed,_:=result["isError"].(bool);failed{writeError(w,502,"Compute query unavailable");return}
 items,ok:=result["content"].([]any)
 if !ok||len(items)==0{writeError(w,502,"Compute service returned no data");return}
 first,ok:=items[0].(map[string]any)
 if !ok{writeError(w,502,"invalid compute result");return}
 text,ok:=first["text"].(string)
 if !ok||len(text)>2<<20{writeError(w,502,"invalid compute result");return}
 var output any
 if err:=json.Unmarshal([]byte(text),&output);err!=nil{writeError(w,502,"invalid compute JSON");return}
 writeJSON(w,200,output)
}
