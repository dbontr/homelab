package googlehealth

import(
 "context"
 "errors"
 "fmt"
 "log"
 "strings"
 "time"

 "github.com/dbontr/homelab-new/system/health/internal/health"
)

type PollConfig struct {
 DataTypes []string
 Interval time.Duration
 Lookback time.Duration
 MaxPagesPerType int
}
type PollResult struct {
 DataType string
 Imported int
 Existing int
 NextPageToken string
}
func ValidatePollConfig(cfg PollConfig)error{
 if cfg.Interval<15*time.Minute||cfg.Interval>24*time.Hour{return errors.New("poll interval must be 15 minutes to 24 hours")}
 if cfg.Lookback<time.Hour||cfg.Lookback>30*24*time.Hour{return errors.New("poll lookback must be 1 hour to 30 days")}
 if cfg.MaxPagesPerType<1||cfg.MaxPagesPerType>20{return errors.New("page count must be 1 to 20")}
 if len(cfg.DataTypes)<1||len(cfg.DataTypes)>25{return errors.New("poll 1 to 25 data types")}
 seen:=map[string]bool{}
 for _,kind:=range cfg.DataTypes{
  if !supported[kind]||seen[kind]{return fmt.Errorf("unsupported or repeated data type %q",kind)}
  seen[kind]=true
 }
 return nil
}
func BuildTimeFilter(kind string,after time.Time)(string,error){
 if !supported[kind]{return "",errors.New("unsupported data type")}
 field:=""
 switch kind{
 case "sleep":field="sleep.interval.end_time"
 case "exercise":field="exercise.interval.civil_start_time"
 case "steps","distance","total-calories","active-minutes","active-energy-burned":
  field=strings.ReplaceAll(kind,"-","_")+".interval.start_time"
 case "weight","body-fat","heart-rate","heart-rate-variability","oxygen-saturation",
  "blood-glucose","vo2-max","run-vo2-max","hydration-log","nutrition-log":
  field=strings.ReplaceAll(kind,"-","_")+".sample_time.physical_time"
 case "daily-resting-heart-rate","daily-heart-rate-variability","daily-respiratory-rate",
  "daily-oxygen-saturation","daily-vo2-max":
  field=strings.ReplaceAll(kind,"-","_")+".date"
 default:return "",fmt.Errorf("unsupported filter for %s",kind)
 }
 stamp:=after.UTC().Format(time.RFC3339)
 if strings.HasSuffix(field,".date")||strings.Contains(field,".civil_start_time"){
  stamp=after.UTC().Format("2006-01-02")
 }
 return field+" >= \""+stamp+"\"",nil
}
func(c *Client)PollOnce(ctx context.Context,store *health.Store,cfg PollConfig)([]PollResult,error){
 if err:=ValidatePollConfig(cfg);err!=nil{return nil,err}
 results:=[]PollResult{}
 for _,kind:=range cfg.DataTypes{
  filter,token:="",""
  cursor,err:=store.GetSyncCursor(ctx,"google_health:"+kind)
  if err==nil{filter=cursor.Filter;token=cursor.PageToken
  }else if !health.IsSyncCursorMissing(err){return results,err}
  if filter==""{
   filter,err=BuildTimeFilter(kind,time.Now().UTC().Add(-cfg.Lookback))
   if err!=nil{return results,err}
  }
  summary,err:=c.Sync(ctx,store,kind,filter,token,cfg.MaxPagesPerType)
  if err!=nil{
   if token!=""{_=store.ClearSyncCursor(ctx,"google_health:"+kind)}
   return results,fmt.Errorf("Google Health %s sync: %w",kind,err)
  }
  if summary.NextPageToken!=""{
   if err=store.PutSyncCursor(ctx,"google_health:"+kind,filter,summary.NextPageToken);err!=nil{return results,err}
  }else if err=store.ClearSyncCursor(ctx,"google_health:"+kind);err!=nil{return results,err}
  results=append(results,PollResult{DataType:kind,Imported:summary.Imported,Existing:summary.Existing,
   NextPageToken:summary.NextPageToken})
 }
 return results,nil
}
func(c *Client)RunPoller(ctx context.Context,store *health.Store,cfg PollConfig)error{
 if err:=ValidatePollConfig(cfg);err!=nil{return err}
 run:=func(){
  timeoutCtx,cancel:=context.WithTimeout(ctx,20*time.Minute)
  defer cancel()
  results,err:=c.PollOnce(timeoutCtx,store,cfg)
  if err!=nil{log.Printf("Google Health read-only sync failed: %v",err);return}
  for _,result:=range results{
   log.Printf("Google Health %s: %d imported, %d already stored, additional pages %t",
    result.DataType,result.Imported,result.Existing,result.NextPageToken!="")
  }
 }
 run()
 ticker:=time.NewTicker(cfg.Interval);defer ticker.Stop()
 for{
  select{
  case <-ctx.Done():return ctx.Err()
  case <-ticker.C:run()
  }
 }
}
