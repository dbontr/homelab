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
 default:writeError(w,404,"unknown project view");return
 }
 raw,err:=json.Marshal(map[string]string{"project_id":project})
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
