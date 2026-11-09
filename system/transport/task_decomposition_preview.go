package transport

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "io"
 "net/http"
 "os"

 "github.com/dbontr/homelab-new/system/agentflow"
 "github.com/dbontr/homelab-new/system/store"
)

type TaskDecompositionPreview struct{
 ParentID string `json:"parent_id"`
 ExpectedRevision int `json:"expected_revision"`
 Steps []store.TaskStepInput `json:"steps"`
 TotalMinutes int `json:"total_minutes"`
 RequiresOwnerSubmission bool `json:"requires_owner_submission"`
 SharedWithPlanningModel []string `json:"shared_with_planning_model"`
}

func(s *Server)taskDecompositionPreview(ctx context.Context,parentID string,revision int,shareTitle bool)(TaskDecompositionPreview,error){
 var proposal TaskDecompositionPreview
 if !shareTitle {
  return proposal,errors.New("task title and duration disclosure requires explicit consent")
 }
 planner,ok:=s.capabilityPlanner.(agentflow.TaskBreakdownPlanner)
 if !ok{return proposal,errors.New("task planning model unavailable")}
 if revision<1{return proposal,store.ErrTaskRevision}
 var parent store.Task
 found:=false
 for _,task:=range s.db.Tasks(""){
  if task.ID==parentID{parent=task;found=true;break}
 }
 if !found{return proposal,os.ErrNotExist}
 if parent.Status!="open"||parent.Decomposed||parent.Revision!=revision{
  return proposal,store.ErrTaskRevision
 }
 raw,err:=planner.ProposeTaskSteps(ctx,parent.Title,parent.Minutes)
 if err!=nil{return proposal,err}
 if len(raw)>24<<10{return proposal,errors.New("task proposal exceeds response limit")}
 var parsed struct{Steps []store.TaskStepInput `json:"steps"`}
 decoder:=json.NewDecoder(bytes.NewReader(raw))
 decoder.DisallowUnknownFields()
 if err:=decoder.Decode(&parsed);err!=nil{
  return proposal,errors.New("planning model returned invalid task steps")
 }
 var extra any
 if err:=decoder.Decode(&extra);err!=io.EOF{
  return proposal,errors.New("planning model returned extra task data")
 }
 if len(parsed.Steps)>12{
  return proposal,errors.New("planning model exceeded maximum task steps")
 }
 if err:=store.ValidateTaskDecompositionSteps(parent,parsed.Steps);err!=nil{
  return proposal,err
 }
 // Model latency cannot silently approve a stale parent task.
 unchanged:=false
 for _,task:=range s.db.Tasks(""){
  if task.ID==parentID&&task.Status=="open"&&!task.Decomposed&&
   task.Revision==revision{unchanged=true;break}
 }
 if !unchanged{return proposal,store.ErrTaskRevision}
 proposal=TaskDecompositionPreview{
  ParentID:parentID,ExpectedRevision:revision,Steps:parsed.Steps,
  RequiresOwnerSubmission:true,
  SharedWithPlanningModel:[]string{"title","minutes"},
 }
 for _,step:=range parsed.Steps{proposal.TotalMinutes+=step.Minutes}
 return proposal,nil
}

func(s *Server)uiTaskDecompositionPreview(w http.ResponseWriter,r *http.Request){
 var input struct {
  ExpectedRevision int `json:"expected_revision"`
  ShareTitleWithModel bool `json:"share_title_with_model"`
 }
 if err:=decode(r,&input);err!=nil{writeError(w,400,"invalid task planning request");return}
 result,err:=s.taskDecompositionPreview(r.Context(),r.PathValue("id"),input.ExpectedRevision,input.ShareTitleWithModel)
 switch{
 case errors.Is(err,os.ErrNotExist):writeError(w,404,"parent task not found")
 case errors.Is(err,store.ErrTaskRevision):writeError(w,409,"parent task revision conflict")
 case s.capabilityPlanner==nil:writeError(w,503,"planning model not configured")
 case err!=nil:writeError(w,422,"task decomposition proposal unavailable or invalid")
 default:writeJSON(w,http.StatusOK,result)
 }
}
