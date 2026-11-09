package controller

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
 "strings"
 "time"
 "unicode/utf8"
)

// EmbeddingProvider sends research text only for libraries whose owner has
// explicitly enabled embedding. Provider responses are untrusted model data.
type EmbeddingProvider struct {
 Endpoint string
 Model string
 Token string
 Client *http.Client
}

func NewEmbeddingProvider(endpoint,model,token string)(*EmbeddingProvider,error){
 u,err:=url.Parse(endpoint)
 if err!=nil||u.Host==""||u.User!=nil||u.RawQuery!=""||u.Fragment!=""||
  u.Path!="/v1/embeddings"||!embeddingModelID.MatchString(model)||strings.ContainsAny(token,"\x00\r\n"){
  return nil,errors.New("invalid embedding provider configuration")
 }
 host:=u.Hostname()
 ip:=net.ParseIP(host)
 local:=host=="localhost"||(ip!=nil&&ip.IsLoopback())
 if u.Scheme!="https" && !(u.Scheme=="http"&&local){
  return nil,errors.New("embedding provider requires HTTPS or loopback HTTP")
 }
 return &EmbeddingProvider{Endpoint:endpoint,Model:model,Token:token,
  Client:&http.Client{Timeout:20*time.Second,CheckRedirect:func(*http.Request,[]*http.Request)error{
   return errors.New("embedding provider redirects disabled")
  }}},nil
}

func (p *EmbeddingProvider) Embed(ctx context.Context,text string)([]float64,error){
 if p==nil||p.Client==nil||len(text)<1||len(text)>8192||
  !utf8.ValidString(text)||strings.ContainsRune(text,0){
  return nil,errors.New("invalid text for embedding")
 }
 body,_:=json.Marshal(map[string]any{"model":p.Model,"input":text,"encoding_format":"float"})
 request,err:=http.NewRequestWithContext(ctx,http.MethodPost,p.Endpoint,bytes.NewReader(body))
 if err!=nil{return nil,err}
 request.Header.Set("Content-Type","application/json")
 if p.Token!=""{request.Header.Set("Authorization","Bearer "+p.Token)}
 response,err:=p.Client.Do(request)
 if err!=nil{return nil,errors.New("embedding provider unavailable")}
 defer response.Body.Close()
 if response.StatusCode!=http.StatusOK{return nil,fmt.Errorf("embedding provider returned HTTP %d",response.StatusCode)}
 var reply struct{
  Model string `json:"model"`
  Data []struct{
   Index int `json:"index"`
   Embedding []float64 `json:"embedding"`
  } `json:"data"`
 }
 encoded,err:=io.ReadAll(io.LimitReader(response.Body,(256<<10)+1))
 if err!=nil||len(encoded)>256<<10{return nil,errors.New("embedding provider response exceeds bound")}
 decoder:=json.NewDecoder(bytes.NewReader(encoded))
 if err:=decoder.Decode(&reply);err!=nil{return nil,errors.New("invalid embedding provider response")}
 if err:=decoder.Decode(new(any));err!=io.EOF{return nil,errors.New("embedding response has trailing content")}
 if len(reply.Data)!=1||reply.Data[0].Index!=0||(reply.Model!=""&&reply.Model!=p.Model){
  return nil,errors.New("embedding provider returned inconsistent model or indexes")
 }
 if _,err:=vectorNorm(reply.Data[0].Embedding);err!=nil{return nil,err}
 return reply.Data[0].Embedding,nil
}
