package privileged

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "io"
 "net"
 "net/http"
 "path/filepath"
 "time"

 "github.com/dbontr/homelab-new/system/security"
)

type Client struct{
 http *http.Client
}

func NewClient(socket string)(*Client,error){
 if !filepath.IsAbs(socket){return nil,errors.New("executor socket must be absolute")}
 transport:=&http.Transport{DisableKeepAlives:true,DialContext:func(ctx context.Context,_,_ string)(net.Conn,error){
  return (&net.Dialer{Timeout:2*time.Second}).DialContext(ctx,"unix",socket)
 }}
 return &Client{http:&http.Client{Transport:transport,Timeout:180*time.Second}},nil
}

func(c *Client) Execute(ctx context.Context,operation,target string,args json.RawMessage)(security.Authorization,error){
 payload,err:=json.Marshal(map[string]any{"operation":operation,"target":target,"arguments":args})
 if err!=nil{return security.Authorization{},err}
 req,err:=http.NewRequestWithContext(ctx,http.MethodPost,"http://unix/v1/execute",bytes.NewReader(payload))
 if err!=nil{return security.Authorization{},err}
 req.Header.Set("Content-Type","application/json")
 req.Header.Set("Cf-Access-Jwt-Assertion",security.AccessAssertion(ctx))
 response,err:=c.http.Do(req)
 if err!=nil{return security.Authorization{},err}
 defer response.Body.Close()
 if response.StatusCode!=http.StatusOK{return security.Authorization{},errors.New("executor refused operation")}
 var value security.Authorization
 raw,err:=io.ReadAll(io.LimitReader(response.Body,8193))
 if err!=nil||len(raw)>8192{return security.Authorization{},errors.New("invalid executor reply")}
 if err:=json.Unmarshal(raw,&value);err!=nil{return security.Authorization{},err}
 return value,nil
}
