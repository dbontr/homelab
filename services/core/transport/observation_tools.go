package transport

import (
 "context"
 "encoding/json"
 "errors"
 "time"

 "github.com/dbontr/homelab-new/system/calendar"
)

// Monitoring and planning tools use the same domain operations as the browser.
// Inputs select fixed reports only; no arbitrary query, URL or host is accepted.
func(s *Server) monitoringTool(ctx context.Context,name string,raw json.RawMessage)(any,error){
 if s.observability==nil{return nil,errors.New("monitoring is not configured")}
 var args struct{
  Metric string `json:"metric"`
  Range string `json:"range"`
  Search string `json:"search"`
  Limit int `json:"limit"`
 }
 if len(raw)>0&&json.Unmarshal(raw,&args)!=nil{return nil,errors.New("invalid monitoring arguments")}
 switch name{
 case "monitoring_summary":
  return s.observability.Summary(ctx)
 case "monitoring_targets":
  return s.observability.Targets(ctx)
 case "monitoring_alerts":
  return s.observability.Alerts(ctx)
 case "monitoring_security":
  return s.observability.SecurityAudit(ctx)
 case "monitoring_series":
  allowed:=map[string]bool{
   "cpu":true,"memory":true,"disk":true,"gpu":true,
   "gpu-vram":true,"gpu-temp":true,"gpu-age":true,
   "network-rx":true,"network-tx":true,"disk-read":true,
   "disk-write":true,"availability":true,
  }
  if !allowed[args.Metric]{return nil,errors.New("unsupported monitoring metric")}
  if args.Range==""{args.Range="6h"}
  return s.observability.Series(ctx,args.Metric,args.Range)
 case "monitoring_logs":
  if args.Range==""{args.Range="1h"}
  if args.Limit==0{args.Limit=100}
  return s.observability.Logs(ctx,args.Range,args.Search,args.Limit)
 }
 return nil,errors.New("unknown monitoring tool")
}

func(s *Server) calendarTool(ctx context.Context,name string,raw json.RawMessage)(any,error){
 var args struct{
  Start string `json:"start"`
  End string `json:"end"`
  From string `json:"from"`
  DailyMinutes int `json:"daily_minutes"`
  StartHour int `json:"start_hour"`
 }
 if len(raw)>0&&json.Unmarshal(raw,&args)!=nil{return nil,errors.New("invalid planning arguments")}
 switch name{
 case "calendar_occurrences":
  if s.calendarURL==""{return nil,errors.New("calendar is not configured")}
  now:=time.Now().UTC()
  from,until:=now,now.AddDate(0,0,30)
  if args.Start!=""||args.End!=""{
   var errA,errB error
   from,errA=time.Parse(time.RFC3339,args.Start)
   until,errB=time.Parse(time.RFC3339,args.End)
   if errA!=nil||errB!=nil||!until.After(from)||until.Sub(from)>366*24*time.Hour{
    return nil,errors.New("calendar window must be RFC3339 and at most 366 days")
   }
  }
  feed,err:=calendar.Fetch(ctx,s.client,s.calendarURL)
  if err!=nil{return nil,errors.New("calendar feed unavailable")}
  return calendar.Expand(feed,from,until)
 case "weekly_plan":
  if args.DailyMinutes==0{args.DailyMinutes=180}
  if args.StartHour==0{args.StartHour=9}
  return s.planWeek(ctx,args.From,args.DailyMinutes,args.StartHour)
 }
 return nil,errors.New("unknown calendar tool")
}
