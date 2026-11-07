// Package googlehealth provides read-only access to the Google Health v4 REST API.
package googlehealth

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net/http"
 "net/url"
 "strconv"
 "strings"
 "sync"
 "time"

 "github.com/dbontr/personal-health-mcp/internal/health"
)

const defaultBase="https://health.googleapis.com/v4"
const defaultTokenURL="https://oauth2.googleapis.com/token"
const maxResponse=8<<20

var supported=map[string]bool{
 "sleep":true,"steps":true,"weight":true,"body-fat":true,
 "heart-rate":true,"heart-rate-variability":true,
 "daily-heart-rate-variability":true,"daily-resting-heart-rate":true,
 "oxygen-saturation":true,"daily-oxygen-saturation":true,
 "daily-respiratory-rate":true,"exercise":true,"distance":true,
 "vo2-max":true,"run-vo2-max":true,"total-calories":true,
 "active-minutes":true,"hydration-log":true,
 "daily-vo2-max":true,"blood-glucose":true,"active-energy-burned":true,"nutrition-log":true,
}
type Config struct {
 AccessToken string
 RefreshToken string
 ClientID string
 ClientSecret string
 HTTP *http.Client
 // BaseURL and TokenURL are for local integration tests only.
 BaseURL string
 TokenURL string
}
type Client struct {
 cfg Config
 http *http.Client
 base string
 tokenURL string
 mu sync.Mutex
 access string
 expires time.Time
}
type Page struct {
 DataPoints []json.RawMessage `json:"dataPoints"`
 NextPageToken string `json:"nextPageToken"`
}
type SyncResult struct {
 DataType string `json:"data_type"`
 Imported int `json:"imported"`
 Existing int `json:"existing"`
 Pages int `json:"pages"`
 NextPageToken string `json:"next_page_token,omitempty"`
}
func New(cfg Config) (*Client,error) {
 if cfg.AccessToken=="" && (cfg.RefreshToken==""||cfg.ClientID==""||cfg.ClientSecret=="") {
  return nil,errors.New("Google Health requires an access token or OAuth refresh credentials")
 }
 client:=cfg.HTTP
 if client==nil{client=&http.Client{Timeout:30*time.Second,CheckRedirect:func(*http.Request,[]*http.Request) error{return http.ErrUseLastResponse}}}
 base:=defaultBase;token:=defaultTokenURL
 if cfg.BaseURL!="" {base=strings.TrimRight(cfg.BaseURL,"/")}
 if cfg.TokenURL!="" {token=cfg.TokenURL}
 return &Client{cfg:cfg,http:client,base:base,tokenURL:token,access:cfg.AccessToken},nil
}
func (c *Client) accessToken(ctx context.Context) (string,error) {
 c.mu.Lock();defer c.mu.Unlock()
 if c.access!=""&&(c.expires.IsZero()||time.Now().Before(c.expires)){return c.access,nil}
 if c.cfg.RefreshToken=="" {return "",errors.New("Google access token expired or is missing")}
 body:=url.Values{"grant_type":{"refresh_token"},"refresh_token":{c.cfg.RefreshToken},"client_id":{c.cfg.ClientID},"client_secret":{c.cfg.ClientSecret}}
 req,err:=http.NewRequestWithContext(ctx,http.MethodPost,c.tokenURL,strings.NewReader(body.Encode()))
 if err!=nil{return "",err}
 req.Header.Set("Content-Type","application/x-www-form-urlencoded")
 resp,err:=c.http.Do(req);if err!=nil{return "",err}
 defer resp.Body.Close()
 if resp.StatusCode!=200{return "",fmt.Errorf("Google OAuth token endpoint returned HTTP %d",resp.StatusCode)}
 var token struct {AccessToken string `json:"access_token"`; ExpiresIn int `json:"expires_in"`}
 if err=json.NewDecoder(io.LimitReader(resp.Body,1<<20)).Decode(&token);err!=nil{return "",err}
 if token.AccessToken==""||token.ExpiresIn<30{return "",errors.New("Google OAuth returned an invalid access token")}
 c.access=token.AccessToken;c.expires=time.Now().Add(time.Duration(token.ExpiresIn-20)*time.Second)
 return c.access,nil
}
func (c *Client) Page(ctx context.Context,dataType,filter,pageToken string) (Page,error) {
 if !supported[dataType]{return Page{},errors.New("unsupported Google Health data type")}
 token,err:=c.accessToken(ctx);if err!=nil{return Page{},err}
 endpoint,err:=url.Parse(c.base+"/users/me/dataTypes/"+dataType+"/dataPoints")
 if err!=nil{return Page{},err}
 q:=endpoint.Query();q.Set("pageSize","25")
 if filter!=""{if len(filter)>1024{return Page{},errors.New("Google Health filter too long")};q.Set("filter",filter)}
 if pageToken!=""{q.Set("pageToken",pageToken)}
 endpoint.RawQuery=q.Encode()
 for attempt:=0;attempt<2;attempt++{
  req,err:=http.NewRequestWithContext(ctx,http.MethodGet,endpoint.String(),nil)
  if err!=nil{return Page{},err}
  req.Header.Set("Authorization","Bearer "+token)
  req.Header.Set("Accept","application/json")
  resp,err:=c.http.Do(req);if err!=nil{return Page{},err}
  if resp.StatusCode==http.StatusUnauthorized && attempt==0 && c.cfg.RefreshToken!=""{
   _=resp.Body.Close()
   c.mu.Lock();c.access="";c.expires=time.Time{};c.mu.Unlock()
   token,err=c.accessToken(ctx);if err!=nil{return Page{},err}
   continue
  }
  if resp.StatusCode!=http.StatusOK{
   _=resp.Body.Close()
   return Page{},fmt.Errorf("Google Health returned HTTP %d",resp.StatusCode)
  }
  data,err:=io.ReadAll(io.LimitReader(resp.Body,maxResponse+1))
  _=resp.Body.Close()
  if err!=nil{return Page{},err}
  if len(data)>maxResponse{return Page{},errors.New("Google Health response exceeds 8 MiB")}
  var p Page
  if err=json.Unmarshal(data,&p);err!=nil{return Page{},err}
  if p.DataPoints==nil {p.DataPoints=[]json.RawMessage{}}
  return p,nil
 }
 return Page{},errors.New("Google Health authentication retry failed")
}
func find(obj map[string]any,path ...string) any {
 var value any=obj
 for _,k:=range path {
  nested,ok:=value.(map[string]any);if !ok{return nil}
  value=nested[k]
 }
 return value
}
func number(v any)(float64,bool) {
 switch x:=v.(type) {
 case float64:return x,true
 case string:n,err:=strconv.ParseFloat(x,64);return n,err==nil
 default:return 0,false
 }
}
func asTime(v any)(time.Time,bool){
 x,ok:=v.(string);if !ok{return time.Time{},false}
 at,err:=time.Parse(time.RFC3339Nano,x);return at,err==nil
}
func googleDate(value any)(time.Time,bool){
 date,ok:=value.(map[string]any);if !ok{return time.Time{},false}
 yr,yrOK:=number(date["year"]);mo,moOK:=number(date["month"]);day,dayOK:=number(date["day"])
 if !yrOK||!moOK||!dayOK{return time.Time{},false}
 y,m,d:=int(yr),time.Month(int(mo)),int(day)
 if y<1900||y>2200||m<1||m>12||d<1||d>31{return time.Time{},false}
 t:=time.Date(y,m,d,0,0,0,0,time.UTC)
 if t.Year()!=y||t.Month()!=m||t.Day()!=d{return time.Time{},false}
 return t,true
}
func normalize(kind string,raw json.RawMessage)(health.Record,error){
 var x map[string]any
 if err:=json.Unmarshal(raw,&x);err!=nil||x==nil{return health.Record{},errors.New("invalid Google Health data point")}
 id,_:=x["name"].(string)
 if id==""{hash:=sha256.Sum256(raw);id=hex.EncodeToString(hash[:])}
 metric:=strings.ReplaceAll(kind,"-","_")+"_event"
 domain,unit,method:="google_health","record","reported"
 value:=1.0
 timestamp:=time.Time{};precision:="instant"
 for _,key:=range []string{"sleep","exercise","steps","distance","activeEnergyBurned","heartRate","weight","heartRateVariability","bodyFat","bloodGlucose","vo2Max","oxygenSaturation","nutritionLog"}{
  if a,ok:=asTime(find(x,key,"interval","endTime"));ok{timestamp=a;break}
  if a,ok:=asTime(find(x,key,"sampleTime","physicalTime"));ok{timestamp=a;break}
 }
 if timestamp.IsZero(){
  for _,key:=range []string{"dailyRestingHeartRate","dailyHeartRateVariability","dailyVo2Max","dailyOxygenSaturation"}{
   if a,ok:=googleDate(find(x,key,"date"));ok{
    timestamp=a;precision="day";break
   }
  }
 }
 if timestamp.IsZero(){return health.Record{},errors.New("Google Health observation has no recognized timestamp")}
 assign:=func(d,m,u string,v float64,methodName string){
  domain=d;metric=m;unit=u;value=v;method=methodName
 }
 switch kind{
 case "heart-rate":
  if n,ok:=number(find(x,"heartRate","beatsPerMinute"));ok{assign("biometric","heart_rate","bpm",n,"measured")}
 case "weight":
  if n,ok:=number(find(x,"weight","weightGrams"));ok{assign("biometric","weight","kg",n/1000,"measured")}
 case "steps":
  if n,ok:=number(find(x,"steps","count"));ok{assign("activity","steps_interval","count",n,"measured")}
 case "distance":
  if n,ok:=number(find(x,"distance","millimeters"));ok{assign("activity","distance_interval","m",n/1000,"measured")}
 case "sleep":
  if n,ok:=number(find(x,"sleep","summary","minutesAsleep"));ok{assign("sleep","duration_hours","h",n/60,"estimated")}
 case "body-fat":
  if n,ok:=number(find(x,"bodyFat","percentage"));ok{assign("biometric","body_fat_percentage","%",n,"estimated")}
 case "heart-rate-variability":
  if n,ok:=number(find(x,"heartRateVariability","rootMeanSquareOfSuccessiveDifferencesMilliseconds"));ok{
   assign("biometric","hrv_rmssd","ms",n,"measured")
  }else if n,ok:=number(find(x,"heartRateVariability","standardDeviationMilliseconds"));ok{
   assign("biometric","hrv_sdnn","ms",n,"measured")
  }
 case "daily-heart-rate-variability":
  if n,ok:=number(find(x,"dailyHeartRateVariability","averageHeartRateVariabilityMilliseconds"));ok{
   assign("biometric","hrv_daily_average","ms",n,"estimated")
  }
 case "daily-resting-heart-rate":
  if n,ok:=number(find(x,"dailyRestingHeartRate","beatsPerMinute"));ok{
   assign("biometric","resting_heart_rate","bpm",n,"estimated")
  }
 case "vo2-max":
  if n,ok:=number(find(x,"vo2Max","vo2Max"));ok{
   methodName:="estimated"
   if find(x,"vo2Max","measurementMethod")=="METABOLIC_CART"{methodName="measured"}
   assign("fitness","vo2_max","mL/kg/min",n,methodName)
  }
 case "daily-vo2-max":
  if n,ok:=number(find(x,"dailyVo2Max","vo2Max"));ok{
   assign("fitness","vo2_max","mL/kg/min",n,"estimated")
  }
 case "blood-glucose":
  if n,ok:=number(find(x,"bloodGlucose","bloodGlucoseMilligramsPerDeciliter"));ok{
   assign("biometric","blood_glucose","mg/dL",n,"measured")
  }
 case "active-energy-burned":
  if n,ok:=number(find(x,"activeEnergyBurned","kcal"));ok{
   assign("activity","energy_burned_interval","kcal",n,"estimated")
  }
 }
 meta,_:=json.Marshal(map[string]any{
  "google_data_type":kind,"raw":x,"timestamp_precision":precision,
  "note":"Source metric and estimation method retained; device accuracy not independently verified",
 })
 return health.Record{
  Domain:domain,Metric:metric,Value:value,Unit:unit,At:timestamp.UTC(),
  Source:"google_health",SourceID:id,Method:method,Metadata:meta,
 },nil
}
// Sync stores records locally. It only calls Google GET endpoints and OAuth token refresh.
func (c *Client) Sync(ctx context.Context,s *health.Store,kind,filter,pageToken string,maxPages int)(SyncResult,error){
 if maxPages<1||maxPages>20{return SyncResult{},errors.New("maxPages must be 1 to 20")}
 result:=SyncResult{DataType:kind}
 seen:=map[string]bool{}
 for i:=0;i<maxPages;i++{
  page,err:=c.Page(ctx,kind,filter,pageToken);if err!=nil{return result,err}
  result.Pages++
  for _,raw:=range page.DataPoints{
   r,err:=normalize(kind,raw);if err!=nil{return result,err}
   _,err=s.Add(ctx,r)
   if errors.Is(err,health.ErrConflict){result.Existing++;continue}
   if err!=nil{return result,err}
   result.Imported++
  }
  pageToken=page.NextPageToken
  result.NextPageToken=pageToken
  if pageToken==""{break}
  if seen[pageToken]{return result,errors.New("repeated Google Health pagination token")}
  seen[pageToken]=true
 }
 return result,nil
}
