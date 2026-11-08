// Package observability exposes bounded read-only monitoring queries.
package observability

import (
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "math"
 "net"
 "net/http"
 "net/url"
 "sort"
 "strconv"
 "strings"
 "time"
)

const maxResponseBytes = 2 << 20

type Client struct {
 prometheus string
 loki string
 http *http.Client
}

func localEndpoint(raw string) (string,error) {
 if raw=="" {return "",nil}
 u,err:=url.Parse(raw)
 if err!=nil||u.Scheme!="http"||u.User!=nil||u.Opaque!=""||
    (u.Path!=""&&u.Path!="/")||u.RawQuery!=""||u.Fragment!=""||u.Port()=="" {
  return "",errors.New("monitoring source must be loopback HTTP with an explicit port")
 }
 host:=u.Hostname()
 ip:=net.ParseIP(host)
 if host!="localhost"&&(ip==nil||!ip.IsLoopback()){
  return "",errors.New("monitoring source must be loopback")
 }
 port,err:=strconv.Atoi(u.Port())
 if err!=nil||port<1||port>65535{return "",errors.New("invalid monitoring port")}
 return strings.TrimSuffix(u.String(),"/"),nil
}

func New(prometheusURL,lokiURL string)(*Client,error){
 prom,err:=localEndpoint(prometheusURL);if err!=nil{return nil,fmt.Errorf("Prometheus: %w",err)}
 loki,err:=localEndpoint(lokiURL);if err!=nil{return nil,fmt.Errorf("Loki: %w",err)}
 if prom==""&&loki==""{return nil,errors.New("monitoring source not configured")}
 return &Client{prometheus:prom,loki:loki,
  http:&http.Client{
   Timeout:7*time.Second,
   Transport:&http.Transport{Proxy:nil,MaxIdleConnsPerHost:4,ResponseHeaderTimeout:5*time.Second},
   CheckRedirect:func(*http.Request,[]*http.Request)error{return errors.New("monitoring redirects are disabled")},
  },
 },nil
}

func(c *Client) Sources()map[string]bool{
 return map[string]bool{"prometheus":c.prometheus!="","loki":c.loki!=""}
}

func(c *Client) request(ctx context.Context,origin,path string,values url.Values,target any)error{
 if origin==""{return errors.New("monitoring backend is not configured")}
 endpoint:=origin+path
 if values!=nil{endpoint+="?"+values.Encode()}
 req,err:=http.NewRequestWithContext(ctx,http.MethodGet,endpoint,nil)
 if err!=nil{return err}
 req.Header.Set("Accept","application/json")
 response,err:=c.http.Do(req)
 if err!=nil{return fmt.Errorf("monitoring backend unavailable: %w",err)}
 defer response.Body.Close()
 if response.StatusCode!=200{return fmt.Errorf("monitoring backend HTTP %d",response.StatusCode)}
 if response.ContentLength>maxResponseBytes{return errors.New("monitoring response too large")}
 decoder:=json.NewDecoder(io.LimitReader(response.Body,maxResponseBytes+1))
 if err:=decoder.Decode(target);err!=nil{return fmt.Errorf("invalid monitoring response: %w",err)}
 var extra any
 if err:=decoder.Decode(&extra);err!=io.EOF{return errors.New("monitoring response must contain one JSON object")}
 return nil
}

type Point struct{
 At time.Time `json:"at"`
 Value float64 `json:"value"`
}
type Series struct{
 Labels map[string]string `json:"labels"`
 Points []Point `json:"points"`
}
type Measurement struct{
 Key string `json:"key"`
 Label string `json:"label"`
 Unit string `json:"unit"`
 Series []Series `json:"series"`
}
type Snapshot struct{
 At time.Time `json:"at"`
 Sources map[string]bool `json:"sources"`
 Metrics []Measurement `json:"metrics"`
}

type definition struct{ Label,Unit,Query string }
var definitions=map[string]definition{
 "cpu":{"CPU usage","%",`100 * (1 - avg by(instance,node)(rate(node_cpu_seconds_total{mode="idle"}[5m])))`},
 "memory":{"Memory usage","%",`100 * (1 - node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes)`},
 "disk":{"Root filesystem usage","%",`100 * (1 - node_filesystem_avail_bytes{mountpoint="/",fstype!="tmpfs"} / node_filesystem_size_bytes{mountpoint="/",fstype!="tmpfs"})`},
 "network-rx":{"Network received","B/s",`sum by(instance,node)(rate(node_network_receive_bytes_total{device!~"lo|veth.*|br-.*|docker.*"}[5m]))`},
 "network-tx":{"Network transmitted","B/s",`sum by(instance,node)(rate(node_network_transmit_bytes_total{device!~"lo|veth.*|br-.*|docker.*"}[5m]))`},
 "disk-read":{"Disk read","B/s",`sum by(instance,node)(rate(node_disk_read_bytes_total{device=~"nvme.*|sd[a-z]+"}[5m]))`},
 "disk-write":{"Disk write","B/s",`sum by(instance,node)(rate(node_disk_written_bytes_total{device=~"nvme.*|sd[a-z]+"}[5m]))`},
 "gpu":{"GPU utilization","%",`homelab_gpu_utilization_percent`},
 "gpu-vram":{"GPU memory usage","%",`100 * homelab_gpu_memory_used_bytes / homelab_gpu_memory_total_bytes`},
 "gpu-temp":{"GPU temperature","°C",`homelab_gpu_temperature_celsius`},
 "gpu-age":{"GPU sample age","s",`time() - homelab_gpu_sample_timestamp_seconds`},
 "availability":{"Collector availability","up/down",`up`},
}

func sample(pair []json.RawMessage)(Point,bool){
 if len(pair)!=2{return Point{},false}
 var timestamp float64
 var formatted string
 if json.Unmarshal(pair[0],&timestamp)!=nil||json.Unmarshal(pair[1],&formatted)!=nil{return Point{},false}
 value,err:=strconv.ParseFloat(formatted,64)
 if err!=nil||math.IsNaN(value)||math.IsInf(value,0)||math.IsNaN(timestamp)||
    math.IsInf(timestamp,0)||timestamp<0||timestamp>1e12{return Point{},false}
 sec,fraction:=math.Modf(timestamp)
 return Point{At:time.Unix(int64(sec),int64(fraction*1e9)).UTC(),Value:value},true
}

type queryReply struct{
 Status string `json:"status"`
 Data struct{
  ResultType string `json:"resultType"`
  Result []struct{
   Metric map[string]string `json:"metric"`
   Value []json.RawMessage `json:"value"`
   Values [][]json.RawMessage `json:"values"`
  } `json:"result"`
 } `json:"data"`
}

func(c *Client) query(ctx context.Context,key string,window time.Duration)(Measurement,error){
 def,ok:=definitions[key];if !ok{return Measurement{},errors.New("unsupported metric")}
 if c.prometheus==""{return Measurement{},errors.New("Prometheus is not configured")}
 params:=url.Values{"query":{def.Query}}
 path,expected:="/api/v1/query","vector"
 if window>0{
  now:=time.Now().UTC()
  params.Set("start",strconv.FormatInt(now.Add(-window).Unix(),10))
  params.Set("end",strconv.FormatInt(now.Unix(),10))
  step:=int(window.Seconds()/100)
  if step<30{step=30}
  params.Set("step",strconv.Itoa(step))
  path,expected="/api/v1/query_range","matrix"
 }
 var response queryReply
 if err:=c.request(ctx,c.prometheus,path,params,&response);err!=nil{return Measurement{},err}
 if response.Status!="success"||response.Data.ResultType!=expected{return Measurement{},errors.New("invalid metric response")}
 measurement:=Measurement{Key:key,Label:def.Label,Unit:def.Unit,Series:[]Series{}}
 for _,raw:=range response.Data.Result{
  if len(measurement.Series)>=24{break}
  series:=Series{Labels:raw.Metric,Points:[]Point{}}
  if window==0 {
   if point,ok:=sample(raw.Value);ok{series.Points=append(series.Points,point)}
  }else{
   for _,pair:=range raw.Values{
    if point,ok:=sample(pair);ok{series.Points=append(series.Points,point)}
    if len(series.Points)>=150{break}
   }
  }
  if len(series.Points)>0{measurement.Series=append(measurement.Series,series)}
 }
 return measurement,nil
}

// Empty series are unknown measurements. They are not interpreted as zero.
func(c *Client) Summary(ctx context.Context)(Snapshot,error){
 if c.prometheus==""{return Snapshot{},errors.New("Prometheus is not configured")}
 output:=Snapshot{At:time.Now().UTC(),Sources:c.Sources(),Metrics:[]Measurement{}}
 for _,key:=range []string{"cpu","memory","disk","gpu","gpu-vram","gpu-temp","availability"} {
  metric,err:=c.query(ctx,key,0);if err!=nil{return Snapshot{},err}
  output.Metrics=append(output.Metrics,metric)
 }
 return output,nil
}

func(c *Client) Series(ctx context.Context,key,window string)(Measurement,error){
 durations:=map[string]time.Duration{"1h":time.Hour,"6h":6*time.Hour,"24h":24*time.Hour,"7d":7*24*time.Hour}
 duration,ok:=durations[window]
 if !ok{return Measurement{},errors.New("unsupported time range")}
 return c.query(ctx,key,duration)
}

type Target struct{
 Job string `json:"job"`
 Instance string `json:"instance"`
 Health string `json:"health"`
 LastScrape time.Time `json:"last_scrape,omitempty"`
 LastError string `json:"last_error,omitempty"`
}
func(c *Client) Targets(ctx context.Context)([]Target,error){
 var response struct{
  Status string `json:"status"`
  Data struct{
   ActiveTargets []struct{
    Labels map[string]string `json:"labels"`
    Health string `json:"health"`
    LastScrape time.Time `json:"lastScrape"`
    LastError string `json:"lastError"`
   } `json:"activeTargets"`
  } `json:"data"`
 }
 if err:=c.request(ctx,c.prometheus,"/api/v1/targets",url.Values{"state":{"active"}},&response);err!=nil{return nil,err}
 if response.Status!="success"{return nil,errors.New("invalid monitoring targets")}
 output:=[]Target{}
 for _,target:=range response.Data.ActiveTargets{
  if len(output)>=200{break}
  message:=target.LastError
  if len(message)>200{message=message[:200]}
  output=append(output,Target{Job:target.Labels["job"],Instance:target.Labels["instance"],
   Health:target.Health,LastScrape:target.LastScrape,LastError:message})
 }
 sort.Slice(output,func(i,j int)bool{
  if output[i].Job==output[j].Job{return output[i].Instance<output[j].Instance}
  return output[i].Job<output[j].Job
 })
 return output,nil
}

type Alert struct{
 Name string `json:"name"`
 State string `json:"state"`
 Severity string `json:"severity"`
 Summary string `json:"summary"`
 ActiveAt time.Time `json:"active_at,omitempty"`
}
func(c *Client) Alerts(ctx context.Context)([]Alert,error){
 var response struct{
  Status string `json:"status"`
  Data struct{
   Alerts []struct{
    Labels map[string]string `json:"labels"`
    Annotations map[string]string `json:"annotations"`
    State string `json:"state"`
    ActiveAt time.Time `json:"activeAt"`
   } `json:"alerts"`
  } `json:"data"`
 }
 if err:=c.request(ctx,c.prometheus,"/api/v1/alerts",nil,&response);err!=nil{return nil,err}
 if response.Status!="success"{return nil,errors.New("invalid alert response")}
 output:=[]Alert{}
 for _,alert:=range response.Data.Alerts{
  if len(output)>=200{break}
  title:=alert.Annotations["summary"]
  if len(title)>256{title=title[:256]}
  output=append(output,Alert{Name:alert.Labels["alertname"],State:alert.State,
   Severity:alert.Labels["severity"],Summary:title,ActiveAt:alert.ActiveAt})
 }
 sort.Slice(output,func(i,j int)bool{
  if output[i].State==output[j].State{return output[i].Name<output[j].Name}
  return output[i].State<output[j].State
 })
 return output,nil
}
