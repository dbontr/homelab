package store

import (
 "errors"
 "os"
 "strings"
 "time"
)

// TaskStepInput describes an actionable subtask. Dependencies refer to
// zero-based preceding steps in the same atomic decomposition request.
type TaskStepInput struct {
 Title string `json:"title"`
 Notes string `json:"notes,omitempty"`
 Minutes int `json:"minutes"`
 Energy string `json:"energy,omitempty"`
 Location string `json:"location,omitempty"`
 Priority *int `json:"priority,omitempty"`
 DependsOnSteps []int `json:"depends_on_steps,omitempty"`
}

type TaskDecomposition struct {
 Parent Task `json:"parent"`
 Steps []Task `json:"steps"`
}

var ErrTaskChildrenIncomplete=errors.New("complete every recorded subtask before completing the parent")

// ValidateTaskDecompositionSteps checks a complete proposed graph before it
// is shown to the owner or passed to the atomic task writer.
func ValidateTaskDecompositionSteps(parent Task,steps []TaskStepInput)error{
 if len(steps)<2||len(steps)>32{
  return errors.New("decomposition requires 2 to 32 steps")
 }
 if len(parent.DependsOn)>32{return errors.New("parent has too many dependencies")}
 for index,step:=range steps{
  if strings.TrimSpace(step.Title)==""||len(step.Title)>500||len(step.Notes)>10000||
   step.Minutes<1||step.Minutes>1440{
   return errors.New("each subtask requires a title and a duration from 1 to 1440 minutes")
  }
  if step.Energy!=""&&step.Energy!="low"&&step.Energy!="medium"&&step.Energy!="high"{
   return errors.New("invalid subtask energy")
  }
  if len(step.Location)>200{
   return errors.New("subtask location exceeds limit")
  }
  if step.Priority!=nil&&(*step.Priority<0||*step.Priority>5){
   return errors.New("subtask priority must be 0 through 5")
  }
  if len(step.DependsOnSteps)+len(parent.DependsOn)>32{
   return errors.New("subtask exceeds prerequisite limit")
  }
  seen:=map[int]bool{}
  for _,dependency:=range step.DependsOnSteps{
   if dependency<0||dependency>=index||seen[dependency]{
    return errors.New("subtask prerequisites must name unique preceding steps")
   }
   seen[dependency]=true
  }
 }
 return nil
}

// DecomposeTask stores the parent revision and all new children in one
// transaction. A failed dependency or revision check changes no records.
func (s *Store) DecomposeTask(parentID string,expectedRevision int,steps []TaskStepInput)(TaskDecomposition,error){
 if expectedRevision<1{return TaskDecomposition{},ErrTaskRevision}
 s.mu.Lock()
 defer s.mu.Unlock()
 parent,ok:=s.data.Tasks[parentID]
 if !ok{return TaskDecomposition{},os.ErrNotExist}
 if parent.Status!="open"||parent.Revision!=expectedRevision{
  return TaskDecomposition{},ErrTaskRevision
 }
 if err:=ValidateTaskDecompositionSteps(parent,steps);err!=nil{return TaskDecomposition{},err}
 next:=clone(s.data)
 now:=time.Now().UTC()
 result:=TaskDecomposition{Steps:make([]Task,0,len(steps))}
 for _,step:=range steps{
  key,err:=id()
  if err!=nil{return TaskDecomposition{},err}
  if _,exists:=next.Tasks[key];exists{return TaskDecomposition{},errors.New("task identity collision")}
  deps:=append([]string(nil),parent.DependsOn...)
  for _,position:=range step.DependsOnSteps{
   deps=append(deps,result.Steps[position].ID)
  }
  energy:=step.Energy
  if energy==""{energy=parent.Energy}
  location:=step.Location
  if location==""{location=parent.Location}
  priority:=parent.Priority
  if step.Priority!=nil{priority=*step.Priority}
  child:=Task{
   ID:key,Title:strings.TrimSpace(step.Title),Notes:step.Notes,
   GoalID:parent.GoalID,ParentID:parent.ID,
   Links:append([]string(nil),parent.Links...),DependsOn:deps,
   Due:parent.Due,Minutes:step.Minutes,Energy:energy,Location:location,
   Priority:priority,Status:"open",Revision:1,CreatedAt:now,
  }
  next.Tasks[key]=child
  result.Steps=append(result.Steps,child)
 }
 parent.Decomposed=true
 parent.Revision++
 next.Tasks[parent.ID]=parent
 if err:=s.save(next);err!=nil{return TaskDecomposition{},err}
 parent.Links=append([]string(nil),parent.Links...)
 parent.DependsOn=append([]string(nil),parent.DependsOn...)
 for i:=range result.Steps{
  result.Steps[i].Links=append([]string(nil),result.Steps[i].Links...)
  result.Steps[i].DependsOn=append([]string(nil),result.Steps[i].DependsOn...)
 }
 result.Parent=parent
 return result,nil
}
