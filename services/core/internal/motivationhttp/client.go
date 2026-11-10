package motivationhttp

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "io"
 "net"
 "net/http"
 "net/url"
 "strconv"
 "strings"
 "time"
)

const (
 maxRequest = 16 << 10
 maxResponse = 256 << 10
)

// Client is a private transport for an independently owned Rust domain
// service. It never reads or writes the motivation state file.
type Client struct {
 origin string
 token string
 http *http.Client
}

func New(endpoint, token string)(*Client,error){
 u,err:=url.Parse(endpoint)
 if err!=nil || u.Scheme!="http" || u.User!=nil || u.Host=="" ||
   (u.Path!=""&&u.Path!="/") || u.RawQuery!="" || u.Fragment!="" || u.Opaque!="" {
  return nil,errors.New("motivation endpoint must be an exact loopback HTTP origin")
 }
 ip:=net.ParseIP(u.Hostname())
 if ip==nil || !ip.IsLoopback() {return nil,errors.New("motivation endpoint must use a numeric loopback address")}
 port,err:=strconv.Atoi(u.Port())
 if err!=nil||port<1||port>65535{return nil,errors.New("motivation endpoint requires a valid port")}
 if len(token)<32||len(token)>4096||strings.ContainsAny(token,"\r\n\t \x00"){
  return nil,errors.New("invalid motivation service credential")
 }
 return &Client{
  origin:strings.TrimSuffix(endpoint,"/"),token:token,
  http:&http.Client{
   Timeout:5*time.Second,
   Transport:&http.Transport{Proxy:nil,DialContext:(&net.Dialer{Timeout:2*time.Second}).DialContext},
   CheckRedirect:func(_ *http.Request,_ []*http.Request)error{return errors.New("motivation service redirects are prohibited")},
  },
 },nil
}

func admitted(method,path string)bool{
 switch method+" "+path {
 case "GET /v1/motivation/state",
      "PUT /v1/motivation/profile",
      "POST /v1/motivation/pause",
      "POST /v1/motivation/interventions",
      "POST /v1/motivation/feedback":
  return true
 }
 return false
}

func(c *Client) exchange(ctx context.Context,method,path string,payload []byte)(int,[]byte,error){
 if c==nil{return 0,nil,errors.New("motivation service not configured")}
 if !admitted(method,path){return 0,nil,errors.New("motivation operation is not allowed")}
 if len(payload)>maxRequest{return 0,nil,errors.New("motivation request exceeds limit")}
 req,err:=http.NewRequestWithContext(ctx,method,c.origin+path,bytes.NewReader(payload))
 if err!=nil{return 0,nil,err}
 req.Header.Set("Authorization","Bearer "+c.token)
 if len(payload)>0{req.Header.Set("Content-Type","application/json")}
 response,err:=c.http.Do(req)
 if err!=nil{return 0,nil,errors.New("motivation service unavailable")}
 defer response.Body.Close()
 content,err:=io.ReadAll(io.LimitReader(response.Body,maxResponse+1))
 if err!=nil||len(content)>maxResponse{return 0,nil,errors.New("motivation response exceeds limit")}
 if !json.Valid(content){return 0,nil,errors.New("motivation service returned invalid JSON")}
 return response.StatusCode,content,nil
}

func(c *Client) Call(ctx context.Context,method,path string,payload json.RawMessage)(any,error){
 status,data,err:=c.exchange(ctx,method,path,payload)
 if err!=nil{return nil,err}
 if status<200||status>=300{return nil,errors.New("motivation service did not accept the operation")}
 var output any
 if err:=json.Unmarshal(data,&output);err!=nil{return nil,errors.New("invalid motivation response")}
 return output,nil
}

// Register attaches only the private service's fixed, admitted routes.
// The enclosing platform router must authenticate owner sessions and CSRF.
func Register(mux *http.ServeMux,client *Client){
 if mux==nil{panic("motivation router required")}
 routes:=[]struct{method,path string}{
  {"GET","/v1/motivation/state"},
  {"PUT","/v1/motivation/profile"},
  {"POST","/v1/motivation/pause"},
  {"POST","/v1/motivation/interventions"},
  {"POST","/v1/motivation/feedback"},
 }
 for _,route:=range routes {
  method,path:=route.method,route.path
  mux.HandleFunc(method+" "+path,func(w http.ResponseWriter,r *http.Request){
   w.Header().Set("Content-Type","application/json")
   if client==nil{
    http.Error(w,"{\"error\":\"motivation service not configured\"}",http.StatusServiceUnavailable)
    return
   }
   input,err:=io.ReadAll(io.LimitReader(r.Body,maxRequest+1))
   if err!=nil||len(input)>maxRequest{
    http.Error(w,"{\"error\":\"motivation request exceeds limit\"}",http.StatusRequestEntityTooLarge);return
   }
   if len(input)>0&&!json.Valid(input){
    http.Error(w,"{\"error\":\"invalid motivation JSON\"}",http.StatusBadRequest);return
   }
   status,data,err:=client.exchange(r.Context(),method,path,input)
   if err!=nil{
    http.Error(w,"{\"error\":\"motivation service unavailable\"}",http.StatusServiceUnavailable);return
   }
   w.WriteHeader(status)
   _,_=w.Write(data)
  })
 }
}
