package transport

import (
 "crypto/subtle"
 "errors"
 "net/http"
 "strings"
)

// SetInterfaceTokens binds each optional interface to its own private
// credential. The tokens are transport identities, not owner sessions.
func(s *Server) SetInterfaceTokens(mcpToken,apiToken string,apiWritePaths []string)error{
 if s==nil{return errors.New("domain service is unavailable")}
 for _,token:=range []string{mcpToken,apiToken}{
  if token==""{continue}
  if len(token)<32||len(token)>4096||strings.ContainsAny(token,"\x00\r\n\t ")||
   subtle.ConstantTimeCompare([]byte(token),[]byte(s.token))==1{
   return errors.New("invalid or reused service interface credential")
  }
 }
 if mcpToken!=""&&apiToken!=""&&subtle.ConstantTimeCompare([]byte(mcpToken),[]byte(apiToken))==1{
  return errors.New("MCP and native API service identities must differ")
 }
 if len(apiWritePaths)>32||(apiToken==""&&len(apiWritePaths)>0){
  return errors.New("invalid native API write configuration")
 }
 allowed:=map[string]bool{}
 for _,path:=range apiWritePaths{
  if !validInterfaceWritePath(path)||allowed[path]{
   return errors.New("invalid native API write resource")
  }
  allowed[path]=true
 }
 s.mcpInterfaceToken=mcpToken
 s.apiInterfaceToken=apiToken
 s.apiInterfaceWritePaths=allowed
 return nil
}

func validInterfaceWritePath(path string)bool{
 if !strings.HasPrefix(path,"/v1/")||strings.HasSuffix(path,"/")||
  strings.Contains(path,"//")||strings.ContainsAny(path,"\\%?#\x00")||
  path=="/v1/tools"||strings.HasPrefix(path,"/v1/tools/"){
  return false
 }
 for _,part:=range strings.Split(strings.TrimPrefix(path,"/"),"/"){
  if part==""||part=="."||part==".."{return false}
  for _,char:=range part{
   if !(char>='a'&&char<='z'||char>='A'&&char<='Z'||
    char>='0'&&char<='9'||char=='-'||char=='_'){return false}
  }
 }
 return true
}

func(s *Server) interfaceAuthorized(r *http.Request)bool{
 if s==nil{return false}
 header:=r.Header.Get("Authorization")
 if !strings.HasPrefix(header,"Bearer "){return false}
 if s.mcpInterfaceToken!=""&&r.URL.Path=="/mcp"&&
  r.Method==http.MethodPost&&
  subtle.ConstantTimeCompare([]byte(header),[]byte("Bearer "+s.mcpInterfaceToken))==1{
  return true
 }
 if s.apiInterfaceToken==""||!strings.HasPrefix(r.URL.Path,"/v1/")||
  r.URL.Path=="/v1/tools"||strings.HasPrefix(r.URL.Path,"/v1/tools/")||
  subtle.ConstantTimeCompare([]byte(header),[]byte("Bearer "+s.apiInterfaceToken))!=1{
  return false
 }
 if r.Method==http.MethodGet||r.Method==http.MethodHead{return true}
 if r.Method!=http.MethodPost&&r.Method!=http.MethodPut&&
  r.Method!=http.MethodPatch&&r.Method!=http.MethodDelete{return false}
 for prefix:=range s.apiInterfaceWritePaths{
  if r.URL.Path==prefix||strings.HasPrefix(r.URL.Path,prefix+"/"){return true}
 }
 return false
}
