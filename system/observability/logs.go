package observability

import (
 "context"
 "errors"
 "net/url"
 "sort"
 "strconv"
 "strings"
 "time"
)

type LogEntry struct {
 At time.Time `json:"at"`
 Unit string `json:"unit,omitempty"`
 Host string `json:"host,omitempty"`
 Line string `json:"line"`
}

// Logs exposes bounded system journal streams only. Untrusted search text is
// encoded as a quoted LogQL string literal and cannot alter stream selection.
func(c *Client) Logs(ctx context.Context,window,search string,limit int)([]LogEntry,error){
 durations:=map[string]time.Duration{"1h":time.Hour,"6h":6*time.Hour,"24h":24*time.Hour}
 duration,ok:=durations[window]
 if !ok{return nil,errors.New("unsupported log time range")}
 if limit<1||limit>200{return nil,errors.New("log limit must be 1 to 200")}
 search=strings.TrimSpace(search)
 if len(search)>120||strings.ContainsAny(search,"\r\n\x00"){
  return nil,errors.New("invalid log search")
 }
 query:=`{job="systemd"}`
 if search!=""{query+=" |= "+strconv.Quote(search)}
 now:=time.Now().UTC()
 params:=url.Values{
  "query":{query},
  "start":{strconv.FormatInt(now.Add(-duration).UnixNano(),10)},
  "end":{strconv.FormatInt(now.UnixNano(),10)},
  "direction":{"backward"},
  "limit":{strconv.Itoa(limit)},
 }
 var response struct{
  Status string `json:"status"`
  Data struct{
   ResultType string `json:"resultType"`
   Result []struct{
    Stream map[string]string `json:"stream"`
    Values [][]string `json:"values"`
   } `json:"result"`
  } `json:"data"`
 }
 if err:=c.request(ctx,c.loki,"/loki/api/v1/query_range",params,&response);err!=nil{return nil,err}
 if response.Status!="success"||response.Data.ResultType!="streams"{
  return nil,errors.New("invalid log response")
 }
 output:=make([]LogEntry,0,limit)
 for _,stream:=range response.Data.Result{
  for _,pair:=range stream.Values{
   if len(pair)!=2{continue}
   nanos,err:=strconv.ParseInt(pair[0],10,64);if err!=nil{continue}
   line:=pair[1]
   if len(line)>4096{line=line[:4096]}
   output=append(output,LogEntry{At:time.Unix(0,nanos).UTC(),
     Unit:stream.Stream["unit"],Host:stream.Stream["host"],Line:line})
   if len(output)>=limit{break}
  }
  if len(output)>=limit{break}
 }
 sort.Slice(output,func(i,j int)bool{return output[i].At.After(output[j].At)})
 return output,nil
}
