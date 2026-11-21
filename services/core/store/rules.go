package store

import (
 "errors"
 "fmt"
 "sort"
 "strconv"
 "strings"
 "time"
)

var ErrRuleRevision = errors.New("personal rule revision conflict")
var ErrRuleConflict = errors.New("conflicting active personal rules require owner resolution")

type RuleChange struct {
 ExpectedRevision int `json:"expected_revision"`
 Text string `json:"text"`
 Kind string `json:"kind"`
 Enabled bool `json:"enabled"`
 Paused bool `json:"paused"`
 Priority int `json:"priority"`
 Scope string `json:"scope"`
 GoalID string `json:"goal_id,omitempty"`
 Context string `json:"context,omitempty"`
 Effect string `json:"effect,omitempty"`
 Value string `json:"value,omitempty"`
 Exceptions []string `json:"exceptions,omitempty"`
 ExpiresAt string `json:"expires_at,omitempty"`
}

type Recommendation struct {
 Task Task `json:"task"`
 Rationale []string `json:"rationale"`
}
type RecommendationResult struct {
 Recommendations []Recommendation `json:"recommendations"`
 Excluded []RuleExclusion `json:"excluded"`
 Advisory bool `json:"advisory"`
}
type RuleExclusion struct {
 TaskID string `json:"task_id"`
 RuleKeys []string `json:"rule_keys"`
 Explanation string `json:"explanation"`
}

func validRuleChange(key string, c RuleChange) error {
 if strings.TrimSpace(key)==""||len(key)>80||strings.ContainsAny(key," \n\r\t")||
  strings.TrimSpace(c.Text)==""||len(c.Text)>2000||c.ExpectedRevision<0||
  c.Priority<0||c.Priority>10||len(c.GoalID)>128||len(c.Context)>128||
  len(c.Exceptions)>32 {return errors.New("invalid rule metadata")}
 if c.Kind!="constraint"&&c.Kind!="preference"{return errors.New("rule kind must be constraint or preference")}
 if c.Scope!="global"&&c.Scope!="goal"&&c.Scope!="context"{return errors.New("invalid rule scope")}
 if c.Scope=="goal"&&c.GoalID==""||c.Scope=="context"&&c.Context==""{return errors.New("rule scope needs an identifier")}
 if c.GoalID!=""&&c.Scope!="goal"||c.Context!=""&&c.Scope!="context"{return errors.New("rule scope contains an unrelated identifier")}
 switch c.Effect {
 case "":
  if c.Value!="" {return errors.New("rule value needs a typed effect")}
 case "max_minutes":
  n,e:=strconv.Atoi(c.Value);if e!=nil||n<1||n>1440{return errors.New("max_minutes must be 1..1440")}
 case "exclude_energy","require_energy":
  if c.Value!="low"&&c.Value!="medium"&&c.Value!="high"{return errors.New("invalid excluded energy")}
 case "exclude_location","prefer_location":
  if c.Value==""||len(c.Value)>128{return errors.New("invalid location value")}
 case "prefer_goal":
  if c.Value==""||len(c.Value)>128{return errors.New("invalid preferred goal")}
 default:return errors.New("unknown rule effect")
 }
 if c.Kind=="constraint"&&strings.HasPrefix(c.Effect,"prefer_")||
  c.Kind=="preference"&&c.Effect!=""&&!strings.HasPrefix(c.Effect,"prefer_"){
  return errors.New("rule effect and kind do not match")
 }
 if c.ExpiresAt!="" {if _,e:=time.Parse(time.RFC3339,c.ExpiresAt);e!=nil{return errors.New("rule expiry must be RFC3339")}}
 seen:=map[string]bool{}
 for _,exception:=range c.Exceptions {
  if exception==""||len(exception)>128||seen[exception]||strings.ContainsAny(exception," \n\r\t"){
   return errors.New("invalid rule exception")
  };seen[exception]=true
 }
 return nil
}

func (s *Store) PutRule(key string,c RuleChange)(Rule,error){
 key=strings.TrimSpace(key);c.Text=strings.TrimSpace(c.Text)
 if err:=validRuleChange(key,c);err!=nil{return Rule{},err}
 s.mu.Lock();defer s.mu.Unlock()
 old,exists:=s.data.Rules[key]
 if exists&&old.Revision!=c.ExpectedRevision||!exists&&c.ExpectedRevision!=0{return Rule{},ErrRuleRevision}
 if c.Scope=="goal"{if _,ok:=s.data.Goals[c.GoalID];!ok{return Rule{},errors.New("rule goal does not exist")}}
 if c.Effect=="prefer_goal"{if _,ok:=s.data.Goals[c.Value];!ok{return Rule{},errors.New("preferred goal does not exist")}}
 now:=time.Now().UTC()
 created:=old.CreatedAt;if created.IsZero(){created=now}
 r:=Rule{Key:key,Text:c.Text,Kind:c.Kind,Revision:c.ExpectedRevision+1,
  Enabled:c.Enabled,Paused:c.Paused,Priority:c.Priority,Scope:c.Scope,
  GoalID:c.GoalID,Context:c.Context,Effect:c.Effect,Value:c.Value,
  Exceptions:append([]string(nil),c.Exceptions...),ExpiresAt:c.ExpiresAt,
  CreatedAt:created,UpdatedAt:now}
 next:=clone(s.data);next.Rules[key]=r
 return r,s.save(next)
}

func (s *Store) RuleByKey(key string)(Rule,bool){
 s.mu.Lock();defer s.mu.Unlock()
 r,ok:=s.data.Rules[key];r.Exceptions=append([]string(nil),r.Exceptions...)
 return r,ok
}

func ruleApplies(r Rule,task Task,context string,now time.Time)bool{
 if !r.Enabled||r.Paused{return false}
 if r.ExpiresAt!=""{
  until,err:=time.Parse(time.RFC3339,r.ExpiresAt)
  if err!=nil||!now.Before(until){return false}
 }
 switch r.Scope {
 case "global":
 case "goal":if task.GoalID!=r.GoalID{return false}
 case "context":if context!=r.Context{return false}
 default:return false
 }
 for _,exception:=range r.Exceptions{if exception==task.ID{return false}}
 return true
}

func ruleConflicts(a,b Rule)bool{
 if a.Kind!="constraint"||b.Kind!="constraint"||a.Effect!=b.Effect{return false}
 if a.Effect!="require_energy"{return false} // other constraints can coexist
 if a.Value==b.Value{return false}
 // Different context and different goal restrictions do not overlap.
 if a.Scope=="context"&&b.Scope=="context"&&a.Context!=b.Context{return false}
 if a.Scope=="goal"&&b.Scope=="goal"&&a.GoalID!=b.GoalID{return false}
 return true
}

// Recommend applies only explicit structured rules. Unstructured personal
// principles remain visible but do not become executable policy.
func (s *Store) Recommend(minutes int,energy,location,mode string,limit int)(RecommendationResult,error){
 candidates:=s.Suggest(minutes,energy,location,mode,50)
 rules:=s.Rules()
 now:=time.Now().UTC()
 result:=RecommendationResult{Recommendations:[]Recommendation{},Excluded:[]RuleExclusion{},Advisory:true}
 for _,task:=range candidates {
  active:=[]Rule{}
  for _,rule:=range rules{if ruleApplies(rule,task,location,now)&&rule.Effect!=""{active=append(active,rule)}}
  for i,a:=range active{for _,b:=range active[i+1:]{
   if ruleConflicts(a,b){return RecommendationResult{},fmt.Errorf("%w: %s and %s",ErrRuleConflict,a.Key,b.Key)}
  }}
  rationale:=[]string{"Open actionable task with satisfied prerequisites"}
  if task.GoalID!=""{rationale=append(rationale,"Linked to owner goal "+task.GoalID)}
  if task.Due!=""{rationale=append(rationale,"Due "+task.Due)}
  excluded:=[]string{}
  score:=0
  sort.SliceStable(active,func(i,j int)bool{return active[i].Priority>active[j].Priority})
  for _,rule:=range active {
   switch rule.Effect {
   case "max_minutes":
    bound,_:=strconv.Atoi(rule.Value)
    if task.Minutes>bound{excluded=append(excluded,rule.Key)}
   case "exclude_energy":
    if task.Energy==rule.Value{excluded=append(excluded,rule.Key)}
   case "require_energy":
    if task.Energy!=rule.Value{excluded=append(excluded,rule.Key)}
   case "exclude_location":
    if task.Location==rule.Value{excluded=append(excluded,rule.Key)}
   case "prefer_goal":
    if task.GoalID==rule.Value{score+=rule.Priority+1;rationale=append(rationale,"Preference "+rule.Key+" favors this goal")}
   case "prefer_location":
    if task.Location==rule.Value{score+=rule.Priority+1;rationale=append(rationale,"Preference "+rule.Key+" favors this location")}
   }
  }
  if len(excluded)>0{
   result.Excluded=append(result.Excluded,RuleExclusion{TaskID:task.ID,RuleKeys:excluded,Explanation:"Active owner constraint excludes this recommendation; the task is unchanged"})
   continue
  }
  if score>0{rationale=append(rationale,fmt.Sprintf("Preference score %d",score))}
  result.Recommendations=append(result.Recommendations,Recommendation{Task:task,Rationale:rationale})
 }
 if limit<=0{limit=3};if limit>50{limit=50}
 if len(result.Recommendations)>limit{result.Recommendations=result.Recommendations[:limit]}
 return result,nil
}
