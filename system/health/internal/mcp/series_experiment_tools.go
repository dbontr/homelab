package mcp

import(
 "context"
 "encoding/json"
 "errors"
 "math"
 "time"

 "github.com/dbontr/personal-health-mcp/internal/health"
)
func(s *Server)registerSeriesTools(){
 txt:=func(desc string)map[string]any{return field("string",desc)}
 s.add("health_metric_series","Return dated measurements for charting with source and method; errors on truncated result rather than concealing missing data.",true,false,
 []string{"domain","metric","start_date","end_date"},
 map[string]any{"domain":txt("Observation domain"),"metric":txt("Metric name"),
 "start_date":txt("UTC date YYYY-MM-DD"),"end_date":txt("UTC date YYYY-MM-DD"),
 "limit":field("integer","1 to 10000 points; default 1000")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  d,err:=requiredString(a,"domain");if err!=nil{return nil,err}
  m,err:=requiredString(a,"metric");if err!=nil{return nil,err}
  start,err:=requiredString(a,"start_date");if err!=nil{return nil,err}
  end,err:=requiredString(a,"end_date");if err!=nil{return nil,err}
  n,err:=boundedInt(a,"limit",1000,1,10000);if err!=nil{return nil,err}
  points,err:=s.health.MetricSeries(ctx,d,m,start,end,n);if err!=nil{return nil,err}
  return send(map[string]any{"domain":d,"metric":m,"points":points,"count":len(points)})
 })
}
type ExperimentDefinition struct {
 Title string `json:"title"`
 Hypothesis string `json:"hypothesis"`
 Intervention string `json:"intervention"`
 Domain string `json:"domain"`
 Metric string `json:"metric"`
 BaselineStart string `json:"baseline_start"`
 BaselineEnd string `json:"baseline_end"`
 InterventionStart string `json:"intervention_start"`
 InterventionEnd string `json:"intervention_end"`
 Confounders []string `json:"confounders,omitempty"`
}
func validExperiment(e ExperimentDefinition)error{
 if e.Title==""||len(e.Title)>200||e.Hypothesis==""||len(e.Hypothesis)>2000||
    e.Intervention==""||len(e.Intervention)>2000{
  return errors.New("experiment requires a title, hypothesis and intervention")
 }
 for _,field:=range []string{e.Domain,e.Metric}{
  if field==""||len(field)>64{return errors.New("domain and metric are required")}
 }
 dates:=[]string{e.BaselineStart,e.BaselineEnd,e.InterventionStart,e.InterventionEnd}
 parsed:=make([]time.Time,0,4)
 for _,date:=range dates{
  day,err:=time.Parse("2006-01-02",date)
  if err!=nil{return errors.New("experiment dates must be YYYY-MM-DD")}
  parsed=append(parsed,day)
 }
 if parsed[1].Before(parsed[0])||!parsed[2].After(parsed[1])||parsed[3].Before(parsed[2]){
  return errors.New("baseline and intervention date windows must be ordered and disjoint")
 }
 if parsed[3].Sub(parsed[0]).Hours()/24>365{return errors.New("experiment duration cannot exceed one year")}
 if len(e.Confounders)>50{return errors.New("too many confounders")}
 return nil
}
func analyzeValues(points []health.MetricPoint,unit string)(float64,float64,int,error){
 sum:=0.0;sumsq:=0.0;n:=0
 for _,p:=range points{
  if unit!=p.Unit{return 0,0,0,errors.New("mixed measurement units cannot be analyzed")}
  sum+=p.Value;sumsq+=p.Value*p.Value;n++
 }
 if n==0{return 0,0,0,nil}
 mean:=sum/float64(n);sd:=0.0
 if n>1{sd=math.Sqrt(math.Max(0,(sumsq-sum*sum/float64(n))/float64(n-1)))}
 return mean,sd,n,nil
}
func(s *Server)registerExperimentTools(){
 txt:=func(desc string)map[string]any{return field("string",desc)}
 fields:=map[string]any{
  "title":txt("Experiment name"),"hypothesis":txt("Question to investigate"),
  "intervention":txt("Intervention, dose or behavioral change"),"domain":txt("Outcome domain"),
  "metric":txt("Outcome metric"),"baseline_start":txt("YYYY-MM-DD"),
  "baseline_end":txt("YYYY-MM-DD"),"intervention_start":txt("YYYY-MM-DD"),
  "intervention_end":txt("YYYY-MM-DD"),
  "confounders":map[string]any{"type":"array","items":map[string]any{"type":"string"},
    "description":"Variables that could affect interpretation"},
 }
 s.add("health_experiment_save","Register a self-experiment comparing dated baseline and intervention periods. This protocol does not establish causality.",false,false,
 []string{"title","hypothesis","intervention","domain","metric","baseline_start","baseline_end","intervention_start","intervention_end"},fields,
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  var e ExperimentDefinition
  if err:=decodeFields(a,&e);err!=nil{return nil,err}
  if err:=validExperiment(e);err!=nil{return nil,err}
  raw,_:=json.Marshal(e)
  started,_:=time.Parse("2006-01-02",e.BaselineStart)
  saved,err:=s.health.SaveJournal(ctx,health.JournalEntry{
    Category:"experiment",Title:e.Title,OccurredAt:started,Source:"manual",
    Notes:e.Hypothesis,Attributes:raw,
  },0)
  if err!=nil{return nil,err}
  return send(saved)
 })
 s.add("health_experiment_analyze","Compare baseline and intervention outcome distributions descriptively; reports incomplete evidence without causal claims.",true,false,
 []string{"id"},map[string]any{"id":txt("Experiment journal entry ID")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  id,err:=requiredString(a,"id");if err!=nil{return nil,err}
  journal,err:=s.health.GetJournal(ctx,id);if err!=nil{return nil,err}
  if journal.Category!="experiment"{return nil,errors.New("journal entry is not an experiment")}
  var e ExperimentDefinition
  if err=json.Unmarshal(journal.Attributes,&e);err!=nil{return nil,err}
  if err=validExperiment(e);err!=nil{return nil,err}
  before,err:=s.health.MetricSeries(ctx,e.Domain,e.Metric,e.BaselineStart,e.BaselineEnd,10000)
  if err!=nil{return nil,err}
  after,err:=s.health.MetricSeries(ctx,e.Domain,e.Metric,e.InterventionStart,e.InterventionEnd,10000)
  if err!=nil{return nil,err}
  report:=map[string]any{"id":id,"title":e.Title,"baseline_samples":len(before),
    "intervention_samples":len(after),"hypothesis":e.Hypothesis,
    "confounders":e.Confounders,
    "interpretation":"Descriptive observational comparison only; there is no verified causal attribution"}
  if len(before)<3||len(after)<3{
    report["status"]="insufficient_samples";return send(report)
  }
  unit:=before[0].Unit
  bm,bs,bn,err:=analyzeValues(before,unit);if err!=nil{return nil,err}
  am,as,an,err:=analyzeValues(after,unit);if err!=nil{return nil,err}
  report["status"]="descriptive";report["unit"]=unit
  report["baseline_mean"]=bm;report["baseline_sd"]=bs;report["baseline_n"]=bn
  report["intervention_mean"]=am;report["intervention_sd"]=as;report["intervention_n"]=an
  report["difference"]=am-bm
  report["warning"]="Time trends, autocorrelation, expectation and confounding are not controlled by this comparison"
  return send(report)
 })
}
