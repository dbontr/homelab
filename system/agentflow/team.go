package agentflow

import (
 "encoding/json"
 "errors"
 "fmt"
 "context"
 "time"
 "os"
 "path/filepath"
 "sort"
 "strings"
)

type TeamTask struct {
 Key string `json:"key"`
 Role string `json:"role"`
 Objective string `json:"objective"`
 AllowedPaths []string `json:"allowed_paths"`
 DependsOn []string `json:"depends_on,omitempty"`
}
type TeamRequest struct {
 Base Program `json:"base"`
 Tasks []TeamTask `json:"tasks"`
 AutoPlan bool `json:"auto_plan,omitempty"`
}
type TeamMember struct {
 Key string `json:"key"`
 Role string `json:"role"`
 RunID string `json:"run_id"`
 AllowedPaths []string `json:"allowed_paths"`
 DependsOn []string `json:"depends_on,omitempty"`
}
type TeamRecord struct {
 ID string `json:"id"`
 Project string `json:"project"`
 Repository string `json:"repository"`
 Revision string `json:"revision"`
 BaseBranch string `json:"base_branch"`
 ImagePoolID string `json:"image_pool_id"`
 CPU float64 `json:"cpu"`
 MemoryMB int `json:"memory_mb"`
 DurationMinutes int `json:"duration_minutes"`
 Checks []string `json:"checks"`
 Members []TeamMember `json:"members"`
 IntegrationRunID string `json:"integration_run_id,omitempty"`
}
type TeamRun struct {
 Key string `json:"key"`
 Role string `json:"role"`
 Run RunSummary `json:"run"`
}
type TeamArtifact struct {
 Path string `json:"path"`
 ArtifactID string `json:"artifact_id"`
 Member string `json:"member"`
}
type TeamSummary struct {
 ID string `json:"id"`
 Project string `json:"project"`
 Repository string `json:"repository"`
 Revision string `json:"revision"`
 Status Status `json:"status"`
 Members []TeamRun `json:"members"`
 Artifacts []TeamArtifact `json:"artifacts,omitempty"`
 Conflicts []string `json:"conflicts,omitempty"`
 ReadyForReview bool `json:"ready_for_review"`
 Integration *RunSummary `json:"integration,omitempty"`
}
type TeamManager interface {
 SubmitTeam(TeamRequest)(TeamSummary,error)
 GetTeam(string)(TeamSummary,error)
 ListTeams()([]TeamSummary,error)
 CancelTeam(string)error
 IntegrateTeam(string, TeamIntegrationRequest)(TeamSummary,error)
}

func validTeam(req TeamRequest)error{
 if len(req.Tasks)<2||len(req.Tasks)>4||!validID(req.Base.Project)||
   !repositoryName.MatchString(req.Base.Repository)||!commitName.MatchString(req.Base.Revision)||
   req.Base.Revision!=req.Base.ExpectedHead {return errors.New("invalid cooperative project scope")}
 seen:=map[string]bool{}
 paths:=map[string]bool{}
 ownerScope:=map[string]bool{}
 for _,p:=range req.Base.AllowedPaths{if !validSourcePath(p)||sensitiveSourcePath(p){return errors.New("invalid team owner scope")};ownerScope[p]=true}
 for _,task:=range req.Tasks{
  if !validID(task.Key)||len(task.Key)>20||seen[task.Key]||!validID(task.Role)||len(task.Role)>24||
   strings.TrimSpace(task.Objective)==""||len(task.Objective)>4096||
   len(task.AllowedPaths)==0||len(task.AllowedPaths)>8 {
   return errors.New("invalid cooperative agent task")
  }
  seen[task.Key]=true
  for _,p:=range task.AllowedPaths{
   if !validSourcePath(p)||sensitiveSourcePath(p)||paths[p]||(len(ownerScope)>0&&!ownerScope[p]){
    return errors.New("cooperative agents require disjoint source paths")
   }
   paths[p]=true
  }
 }
 if err:=validateTaskDependencies(req.Tasks);err!=nil{return err}
 return nil
}
func (e *Engine) SubmitTeam(req TeamRequest)(TeamSummary,error){
 if e==nil||e.Store==nil||e.Compute==nil||!e.AllowedProjects[req.Base.Project]{
  return TeamSummary{},errors.New("cooperative project is not authorized")
 }
 if _,ok:=e.Author.(DecisionAuthor);!ok{return TeamSummary{},errors.New("autonomous model unavailable")}
 if req.AutoPlan {
  if len(req.Tasks)!=0{return TeamSummary{},errors.New("automatic planning cannot override supplied task assignments")}
  planner,ok:=e.Author.(TeamPlanningAuthor);if !ok{return TeamSummary{},errors.New("team planning provider unavailable")}
  ctx,cancel:=context.WithTimeout(context.Background(),45*time.Second)
  tasks,err:=planner.Decompose(ctx,req.Base);cancel()
  if err!=nil{return TeamSummary{},err}
  req.Tasks=tasks
 }
 if req.Base.Review {if _,ok:=e.Author.(ReviewAuthor);!ok{return TeamSummary{},errors.New("review provider unavailable")}}
 if err:=validTeam(req);err!=nil{return TeamSummary{},err}
 token,err:=randomToken();if err!=nil{return TeamSummary{},err}
 id:="team-"+token[:16]
 team:=TeamRecord{ID:id,Project:req.Base.Project,Repository:req.Base.Repository,Revision:req.Base.Revision,
   BaseBranch:req.Base.BaseBranch,ImagePoolID:req.Base.ImagePoolID,CPU:req.Base.CPU,MemoryMB:req.Base.MemoryMB,DurationMinutes:req.Base.DurationMinutes,Checks:append([]string(nil),req.Base.Checks...)}
 children:=make([]Workflow,0,len(req.Tasks))
 for _,task:=range req.Tasks{
  p:=req.Base
  p.ID=id+"-"+task.Key
  p.Title=task.Role+": "+task.Key
  p.Objective=task.Objective
  p.AllowedPaths=append([]string(nil),task.AllowedPaths...)
  p.Changes=nil
  p.Autonomous=true
  p.PrepareOnly=true
  w,err:=Compile(p)
  if err!=nil{return TeamSummary{},fmt.Errorf("invalid member %s: %w",task.Key,err)}
  children=append(children,w)
  team.Members=append(team.Members,TeamMember{Key:task.Key,Role:task.Role,RunID:w.ID,AllowedPaths:append([]string(nil),task.AllowedPaths...),DependsOn:append([]string(nil),task.DependsOn...)})
 }
 if err:=e.Store.createTeamBatch(team,children);err!=nil{return TeamSummary{},err}
 return e.GetTeam(id)
}
func (s *Store) teamPath(id string)string{return filepath.Join(s.dir,".team-"+id+".json")}
func (s *Store) createTeamBatch(team TeamRecord, children []Workflow)(err error){
 if !validID(team.ID)||len(children)!=len(team.Members){return errors.New("invalid team record")}
 unlock,err:=s.lock();if err!=nil{return err};defer unlock()
 entries,err:=os.ReadDir(s.dir);if err!=nil{return err}
 count:=0
 for _,entry:=range entries{if strings.HasSuffix(entry.Name(),".json")&&!entry.IsDir(){count++}}
 if count+len(children)+1>1000{return errors.New("engineering record capacity reached")}
 for _,w:=range children{
  if err:=Validate(w);err!=nil{return err}
  if _,statErr:=os.Lstat(s.filename(w.ID));statErr==nil{return os.ErrExist}else if !errors.Is(statErr,os.ErrNotExist){return statErr}
 }
 saved:=[]string{}
 defer func(){
  if err!=nil{for _,p:=range saved{_ = os.Remove(p)}}
 }()
 for _,w:=range children{
  w.Revision=1
  for i:=range w.Steps{w.Steps[i].Status=Pending}
  if err=s.save(w);err!=nil{return err}
  saved=append(saved,s.filename(w.ID))
 }
 raw,err:=json.Marshal(team);if err!=nil{return err}
 if len(raw)>128<<10{return errors.New("team record is too large")}
 f,err:=os.OpenFile(s.teamPath(team.ID),os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600);if err!=nil{return err}
 saved=append(saved,s.teamPath(team.ID))
 if _,err=f.Write(raw);err!=nil{f.Close();return err}
 if err=f.Sync();err!=nil{f.Close();return err}
 if err=f.Close();err!=nil{return err}
 dir,err:=os.Open(s.dir);if err!=nil{return err};defer dir.Close()
 return dir.Sync()
}
func (s *Store) readTeam(id string)(TeamRecord,error){
 var team TeamRecord
 if !validID(id){return team,errors.New("invalid team ID")}
 unlock,err:=s.lock();if err!=nil{return team,err};defer unlock()
 f,err:=os.Open(s.teamPath(id));if err!=nil{return team,err};defer f.Close()
 fi,err:=f.Stat();if err!=nil{return team,err}
 if !fi.Mode().IsRegular()||fi.Mode().Perm()&0077!=0||fi.Size()>128<<10{return team,errors.New("invalid team file")}
 if err:=json.NewDecoder(f).Decode(&team);err!=nil{return team,err}
 if team.ID!=id{return team,errors.New("team identity mismatch")}
 return team,nil
}
func (s *Store) teamIDs()([]string,error){
 unlock,err:=s.lock();if err!=nil{return nil,err};defer unlock()
 entries,err:=os.ReadDir(s.dir);if err!=nil{return nil,err}
 ids:=[]string{}
 for _,entry:=range entries{
  n:=entry.Name()
  if entry.IsDir()||!strings.HasPrefix(n,".team-")||!strings.HasSuffix(n,".json"){continue}
  id:=strings.TrimSuffix(strings.TrimPrefix(n,".team-"),".json")
  if validID(id){ids=append(ids,id)}
 }
 if len(ids)>100{return nil,errors.New("team limit exceeded")}
 sort.Strings(ids)
 return ids,nil
}
// GetTeam reconciles immutable output identities from independently verified
// member workflows. No source is combined or published by this read operation.
func (e *Engine) GetTeam(id string)(TeamSummary,error){
 if e==nil||e.Store==nil{return TeamSummary{},errors.New("engineering store unavailable")}
 record,err:=e.Store.readTeam(id);if err!=nil{return TeamSummary{},err}
 if !e.AllowedProjects[record.Project]{return TeamSummary{},errors.New("team project not authorized")}
 team:=TeamSummary{ID:id,Project:record.Project,Repository:record.Repository,Revision:record.Revision,Status:Succeeded}
 seen:=map[string]string{}
 failed,cancelled,running,pending:=false,false,false,false
 for _,member:=range record.Members{
  run,err:=e.Get(member.RunID);if err!=nil{return TeamSummary{},err}
  team.Members=append(team.Members,TeamRun{Key:member.Key,Role:member.Role,Run:run})
  switch run.Status{
  case Failed:failed=true
  case Cancelled:cancelled=true
  case Running:running=true
  case Pending:pending=true
  }
  if run.Status!=Succeeded{continue}
  allowed:=map[string]bool{}
  for _,name:=range member.AllowedPaths{allowed[name]=true}
  found:=false
  for _,step:=range run.Steps{
   if step.Kind!="export"||step.Status!=Succeeded{continue}
   found=true
   var result struct{Artifacts map[string]string `json:"artifacts"`}
   if err:=json.Unmarshal(step.Result,&result);err!=nil||len(result.Artifacts)==0{
    team.Conflicts=append(team.Conflicts,member.Key+": missing verified artifacts")
    break
   }
   for name,artifact:=range result.Artifacts{
    if !validSourcePath(name)||sensitiveSourcePath(name)||!allowed[name]||!validID(artifact){
     team.Conflicts=append(team.Conflicts,member.Key+": unauthorized or invalid artifact path")
     continue
    }
    if owner,exists:=seen[name];exists{
     team.Conflicts=append(team.Conflicts,owner+" and "+member.Key+": overlapping source "+name)
     continue
    }
    seen[name]=member.Key
    team.Artifacts=append(team.Artifacts,TeamArtifact{Path:name,ArtifactID:artifact,Member:member.Key})
   }
  }
  if !found{team.Conflicts=append(team.Conflicts,member.Key+": export not completed")}
 }
 switch {
 case failed:team.Status=Failed
 case cancelled:team.Status=Cancelled
 case running:team.Status=Running
 case pending:team.Status=Pending
 }
 sort.Slice(team.Artifacts,func(i,j int)bool{return team.Artifacts[i].Path<team.Artifacts[j].Path})
 sort.Strings(team.Conflicts)
 team.ReadyForReview=team.Status==Succeeded&&len(team.Artifacts)>0&&len(team.Conflicts)==0
 if record.IntegrationRunID!="" {
  execution,err:=e.Get(record.IntegrationRunID)
  if err!=nil{return TeamSummary{},err}
  team.Integration=&execution
  team.ReadyForReview=false
 }
 return team,nil
}
func (e *Engine) ListTeams()([]TeamSummary,error){
 ids,err:=e.Store.teamIDs();if err!=nil{return nil,err}
 out:=make([]TeamSummary,0,len(ids))
 for _,id:=range ids{
  t,err:=e.GetTeam(id)
  if err!=nil{continue}
  out=append(out,t)
 }
 return out,nil
}
func (e *Engine) CancelTeam(id string)error{
 team,err:=e.Store.readTeam(id);if err!=nil{return err}
 if !e.AllowedProjects[team.Project]{return errors.New("team project not authorized")}
 for _,member:=range team.Members{
  run,err:=e.Get(member.RunID);if err!=nil{return err}
  if run.Status!=Pending&&run.Status!=Running{continue}
  if err:=e.Cancel(run.ID,run.Revision);err!=nil{return err}
 }
 return nil
}
