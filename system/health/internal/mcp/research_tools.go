package mcp

import(
 "context"
 "encoding/json"
 "errors"
 "net/url"
 "strings"
 "time"

 "github.com/dbontr/homelab-new/system/health/internal/health"
)
type ResearchSource struct{
 Title string `json:"title"`
 URL string `json:"url"`
 StudyType string `json:"study_type,omitempty"`
 Year int `json:"year,omitempty"`
}
type Recommendation struct{
 Subject string `json:"subject"`
 Claim string `json:"claim"`
 Rationale string `json:"rationale"`
 Limitations string `json:"limitations"`
 Sources []ResearchSource `json:"sources"`
}
func(s *Server)registerResearchTools(){
 txt:=func(d string)map[string]any{return field("string",d)}
 s.add("health_research_record","Save a proposed evidence-grounded health recommendation with supporting source links, methodological limitations, and unverified citation status. Not medical advice.",false,false,
 []string{"subject","claim","rationale","limitations","sources"},
 map[string]any{"subject":txt("Exercise, nutrition or recovery topic"),"claim":txt("Qualified recommendation"),
 "rationale":txt("Mechanism and reported evidence"),"limitations":txt("Evidence limitations and applicability"),
 "sources":map[string]any{"type":"array","items":map[string]any{"type":"object",
   "properties":map[string]any{"title":txt("Study title"),"url":txt("Official study URL or DOI URL"),
    "study_type":txt("Review, randomized trial, observational study or other"),"year":field("integer","Publication year")},
   "required":[]string{"title","url"}}}},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  var r Recommendation
  if err:=decodeFields(a,&r);err!=nil{return nil,err}
  if strings.TrimSpace(r.Subject)==""||strings.TrimSpace(r.Claim)==""||strings.TrimSpace(r.Rationale)==""||strings.TrimSpace(r.Limitations)==""{
   return nil,errors.New("subject, claim, rationale and limitations are required")
  }
  if len(r.Sources)==0||len(r.Sources)>20{return nil,errors.New("at least one and at most twenty sources are required")}
  for _,source:=range r.Sources{
   link,err:=url.Parse(source.URL)
   if err!=nil||link.Scheme!="https"||link.Host==""||link.User!=nil||len(source.URL)>2048||strings.TrimSpace(source.Title)==""{
    return nil,errors.New("each source must have a title and a valid HTTPS URL")
   }
   if source.Year!=0&&(source.Year<1800||source.Year>time.Now().Year()+1){return nil,errors.New("invalid source publication year")}
  }
  raw,err:=json.Marshal(map[string]any{
    "recommendation":r,"verification_status":"unverified","requires_user_review":true,
  });if err!=nil{return nil,err}
  entry,err:=s.health.SaveJournal(ctx,health.JournalEntry{
   Category:"recommendation",Title:r.Subject,OccurredAt:time.Now().UTC(),
   Notes:r.Claim,Attributes:raw,Source:"research_submitted",
  },0)
  if err!=nil{return nil,err}
  return send(entry)
 })
}
