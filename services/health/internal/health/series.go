package health

import(
 "context"
 "errors"
 "fmt"
 "time"
)

type MetricPoint struct{
 At time.Time `json:"at"`
 Value float64 `json:"value"`
 Unit string `json:"unit"`
 Source string `json:"source"`
 Method string `json:"method"`
}
func(s *Store)MetricSeries(ctx context.Context,domain,metric,first,last string,limit int)([]MetricPoint,error){
 if !identifier.MatchString(domain)||!identifier.MatchString(metric){
  return nil,errors.New("domain and metric must be lowercase identifiers")
 }
 start,err:=time.Parse("2006-01-02",first);if err!=nil{return nil,errors.New("start_date must be YYYY-MM-DD")}
 end,err:=time.Parse("2006-01-02",last);if err!=nil{return nil,errors.New("end_date must be YYYY-MM-DD")}
 if end.Before(start)||end.Sub(start).Hours()/24>3660{return nil,errors.New("date range must be ordered and at most ten years")}
 if limit<1||limit>10000{return nil,errors.New("limit must be 1 to 10000")}
 rows,err:=s.db.QueryContext(ctx,`SELECT at,value,unit,source,method FROM records
  WHERE domain=? AND metric=? AND date(at)>=? AND date(at)<=?
  AND id NOT IN (SELECT replaces FROM records WHERE replaces IS NOT NULL)
  ORDER BY at ASC LIMIT ?`,domain,metric,first,last,limit+1)
 if err!=nil{return nil,err}
 defer rows.Close()
 series:=[]MetricPoint{}
 for rows.Next(){
  var p MetricPoint;var timestamp string
  if err=rows.Scan(&timestamp,&p.Value,&p.Unit,&p.Source,&p.Method);err!=nil{return nil,err}
  p.At,err=time.Parse(time.RFC3339Nano,timestamp);if err!=nil{return nil,err}
  series=append(series,p)
 }
 if err=rows.Err();err!=nil{return nil,err}
 if len(series)>limit{return nil,fmt.Errorf("series exceeds %d measurements; reduce the date range",limit)}
 return series,nil
}
