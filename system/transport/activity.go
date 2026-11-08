package transport

import (
 "net/http"
 "errors"
 "regexp"
 "sort"
 "strings"
 "strconv"
 "time"

 "github.com/dbontr/homelab-new/system/store"
)

var resourceIdentifier=regexp.MustCompile(`^[a-zA-Z0-9_-]{1,80}$`)

type ResourceLink struct{
 Relation string `json:"relation"`
 Reference string `json:"reference"`
}
type Resource struct{
 Reference string `json:"reference"`
 Kind string `json:"kind"`
 ID string `json:"id"`
 Owner string `json:"owner"`
 Links []ResourceLink `json:"links"`
 Data any `json:"data"`
}

func resourceRef(kind,id string) string{
 return "homelab:"+kind+":"+id
}
func parseResourceReference(ref string)(string,string,error){
 if len(ref)>190{return "","",errResourceInvalid}
 parts:=strings.Split(ref,":")
 if len(parts)!=3||parts[0]!="homelab"||!resourceIdentifier.MatchString(parts[2]){
  return "","",errResourceInvalid
 }
 switch parts[1]{
 case "task","goal","recipe","meal","book":
  return parts[1],parts[2],nil
 }
 return "","",errResourceInvalid
}

func withReference(kind,id,owner string,data any,links []ResourceLink) Resource{
 if links==nil{links=[]ResourceLink{}}
 return Resource{Reference:resourceRef(kind,id),Kind:kind,ID:id,Owner:owner,Data:data,Links:links}
}

func taskLinks(task store.Task) []ResourceLink{
 links:=[]ResourceLink{}
 if task.GoalID!=""{links=append(links,ResourceLink{Relation:"goal",Reference:resourceRef("goal",task.GoalID)})}
 if task.ParentID!=""{links=append(links,ResourceLink{Relation:"parent",Reference:resourceRef("task",task.ParentID)})}
 for _,id:=range task.DependsOn{
  links=append(links,ResourceLink{Relation:"prerequisite",Reference:resourceRef("task",id)})
 }
 return links
}

// Resolving resources uses their actual owner. There is no shadow record
// that can silently diverge from the canonical task or recipe database.
var errResourceInvalid=errors.New("invalid resource identity or kind")
var errResourceUnavailable=errors.New("resource owner unavailable")
var errResourceNotFound=errors.New("resource not found")

func(s *Server) resource(kind,id string)(Resource,error){
 if !resourceIdentifier.MatchString(id){return Resource{},errResourceInvalid}
 switch kind{
 case "task":
  for _,item:=range s.db.Tasks(""){
   if item.ID==id{return withReference(kind,id,"personal",item,taskLinks(item)),nil}
  }
 case "goal":
  for _,item:=range s.db.Goals(){
   if item.ID==id{return withReference(kind,id,"personal",item,nil),nil}
  }
 case "recipe":
  if s.recipes==nil{return Resource{},errResourceUnavailable}
  item,err:=s.recipes.Get(id)
  if err==nil{return withReference(kind,id,"recipes",item,nil),nil}
 case "meal":
  if s.recipes==nil{return Resource{},errResourceUnavailable}
  for _,item:=range s.recipes.Meals(){
   if item.ID==id{
    links:=[]ResourceLink{{Relation:"recipe",Reference:resourceRef("recipe",item.RecipeID)}}
    return withReference(kind,id,"recipes",item,links),nil
   }
  }
 case "book":
  for _,item:=range s.db.ListBookRequests(""){
   if item.ID==id{return withReference(kind,id,"personal",item,nil),nil}
  }
 default:
  return Resource{},errResourceInvalid
 }
 return Resource{},errResourceNotFound
}

func(s *Server) uiResourceReference(w http.ResponseWriter,r *http.Request){
 kind,id,err:=parseResourceReference(r.URL.Query().Get("ref"))
 if err!=nil{writeError(w,400,err.Error());return}
 item,err:=s.resource(kind,id)
 switch {
 case errors.Is(err,errResourceInvalid):writeError(w,400,err.Error())
 case errors.Is(err,errResourceUnavailable):writeError(w,503,err.Error())
 case err!=nil:writeError(w,404,err.Error())
 default:writeJSON(w,200,item)
 }
}

func(s *Server) uiResource(w http.ResponseWriter,r *http.Request){
 item,err:=s.resource(r.PathValue("kind"),r.PathValue("id"))
 switch {
 case errors.Is(err,errResourceInvalid):writeError(w,400,err.Error())
 case errors.Is(err,errResourceUnavailable):writeError(w,503,err.Error())
 case err!=nil:writeError(w,404,err.Error())
 default:writeJSON(w,200,item)
 }
}

type ActivityEvent struct{
 ID string `json:"id"`
 At time.Time `json:"at"`
 Action string `json:"action"`
 Reference string `json:"reference"`
 Kind string `json:"kind"`
 Summary string `json:"summary"`
}
type ActivityFeed struct{
 At time.Time `json:"at"`
 Events []ActivityEvent `json:"events"`
 Note string `json:"note"`
}

func (s *Server) activity(limit int,after time.Time) ActivityFeed{
 output:=ActivityFeed{
  At:time.Now().UTC(),Events:[]ActivityEvent{},
  Note:"Derived only from recorded personal actions. Unrecorded work and unavailable services are unknown.",
 }
 add:=func(kind,id,action,summary string,at time.Time){
  if at.IsZero()||(!after.IsZero()&&!at.After(after)){return}
  output.Events=append(output.Events,ActivityEvent{
   ID:kind+":"+id+":"+action+":"+strconv.FormatInt(at.UnixNano(),10),
   At:at,Action:action,Kind:kind,Reference:resourceRef(kind,id),Summary:summary,
  })
 }
 for _,task:=range s.db.Tasks(""){
  add("task",task.ID,"created",task.Title,task.CreatedAt)
  if task.Status=="completed"&&task.CompletedAt!=nil{
   add("task",task.ID,"completed",task.Title,*task.CompletedAt)
  }
 }
 for _,goal:=range s.db.Goals(){
  add("goal",goal.ID,"created",goal.Title,goal.CreatedAt)
 }
 for _,book:=range s.db.ListBookRequests(""){
  add("book",book.ID,"requested",book.Title,book.CreatedAt)
  if book.Revision>1&&book.Status!="requested"{
   add("book",book.ID,book.Status,book.Title,book.UpdatedAt)
  }
 }
 if s.recipes!=nil{
  for _,recipe:=range s.recipes.List(""){
   add("recipe",recipe.ID,"created",recipe.Title,recipe.CreatedAt)
   if recipe.Revision>1{
    add("recipe",recipe.ID,"revised",recipe.Title,recipe.UpdatedAt)
   }
  }
  for _,meal:=range s.recipes.Meals(){
   add("meal",meal.ID,"scheduled",meal.RecipeID,meal.CreatedAt)
  }
 }
 sort.SliceStable(output.Events,func(i,j int)bool{
  if output.Events[i].At.Equal(output.Events[j].At){return output.Events[i].ID<output.Events[j].ID}
  return output.Events[i].At.After(output.Events[j].At)
 })
 if len(output.Events)>limit{output.Events=output.Events[:limit]}
 return output
}

func(s *Server) uiActivity(w http.ResponseWriter,r *http.Request){
 limit:=50
 if value:=r.URL.Query().Get("limit");value!=""{
  n,err:=strconv.Atoi(value)
  if err!=nil||n<1||n>200{writeError(w,400,"activity limit must be 1 to 200");return}
  limit=n
 }
 var after time.Time
 if value:=r.URL.Query().Get("since");value!=""{
  parsed,err:=time.Parse(time.RFC3339,value)
  if err!=nil{writeError(w,400,"since must be RFC3339");return}
  after=parsed
 }
 writeJSON(w,200,s.activity(limit,after))
}

