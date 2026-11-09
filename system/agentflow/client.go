package agentflow

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net"
 "net/http"
 "net/url"
 "path/filepath"
 "strings"
 "time"
)

type Client struct{
 socket string
 http *http.Client
}
func NewClient(socket string)(*Client,error){
 if !filepath.IsAbs(socket)||filepath.Ext(socket)!=".sock"||strings.ContainsAny(socket,"\x00\r\n"){
  return nil,errors.New("invalid engineering client socket")
 }
 transport:=&http.Transport{
  DialContext:func(ctx context.Context,_,_ string)(net.Conn,error){
   return (&net.Dialer{Timeout:3*time.Second}).DialContext(ctx,"unix",socket)
  },
  DisableKeepAlives:true,
 }
 return &Client{socket:socket,http:&http.Client{Transport:transport,Timeout:10*time.Second}},nil
}
func (c *Client)request(method,path string,input any,output any)error{
 if c==nil||c.http==nil{return errors.New("engineering service unavailable")}
 var body []byte
 if input!=nil{var err error;body,err=json.Marshal(input);if err!=nil{return err}}
 if len(body)>512<<10{return errors.New("engineering request exceeds limit")}
 req,err:=http.NewRequest(method,"http://unix"+path,bytes.NewReader(body));if err!=nil{return err}
 req.Header.Set("Content-Type","application/json")
 res,err:=c.http.Do(req);if err!=nil{return errors.New("engineering service unavailable")}
 defer res.Body.Close()
 data,err:=io.ReadAll(io.LimitReader(res.Body,512<<10+1))
 if err!=nil||len(data)>512<<10{return errors.New("engineering response exceeds limit")}
 if res.StatusCode<200||res.StatusCode>=300{
  return fmt.Errorf("engineering service rejected request (HTTP %d)",res.StatusCode)
 }
 if err:=json.Unmarshal(data,output);err!=nil{return errors.New("invalid engineering response")}
 return nil
}
func (c *Client)Submit(p Program)(RunSummary,error){
 var out RunSummary
 err:=c.request(http.MethodPost,"/runs",p,&out)
 return out,err
}
func (c *Client)Get(id string)(RunSummary,error){
 var out RunSummary
 if !validID(id){return out,errors.New("invalid engineering run ID")}
 err:=c.request(http.MethodGet,"/runs/"+url.PathEscape(id),nil,&out)
 return out,err
}
func (c *Client)List()([]RunSummary,error){
 var out []RunSummary
 err:=c.request(http.MethodGet,"/runs",nil,&out)
 return out,err
}
func (c *Client)Cancel(id string,revision uint64)error{
 if !validID(id){return errors.New("invalid engineering run ID")}
 var out RunSummary
 return c.request(http.MethodPost,"/runs/"+url.PathEscape(id)+"/cancel",map[string]any{"revision":revision},&out)
}
