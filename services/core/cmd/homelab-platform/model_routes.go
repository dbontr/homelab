package main

import (
 "bytes"
 "encoding/json"
 "errors"
 "io"
 "os"
 "path/filepath"

 "github.com/dbontr/homelab-new/system/agentflow"
 "github.com/dbontr/homelab-new/system/security"
)

type modelRouteSpec struct {
 Name string `json:"name"`
 Purpose string `json:"purpose"`
 Endpoint string `json:"endpoint"`
 Model string `json:"model"`
 TokenFile string `json:"token_file,omitempty"`
 VaultAlias string `json:"vault_alias,omitempty"`
 LocalOnly bool `json:"local_only"`
}

// The route file stores model identifiers and protected credential *paths*,
// never token values. systemd LoadCredential owns token file permissions.
func configuredCapabilityPlanner(vaultClients ...*security.RemoteAuthorizationClient)(agentflow.CapabilityPlanner,error){
 var vault *security.RemoteAuthorizationClient
 if len(vaultClients)>0{vault=vaultClients[0]}
 path:=os.Getenv("HOMELAB_CAPABILITY_MODEL_ROUTES_FILE")
 endpoint:=os.Getenv("HOMELAB_CAPABILITY_MODEL_URL")
 name:=os.Getenv("HOMELAB_CAPABILITY_MODEL_NAME")
 tokenFile:=os.Getenv("HOMELAB_CAPABILITY_MODEL_TOKEN_FILE")
 vaultAlias:=os.Getenv("HOMELAB_CAPABILITY_MODEL_VAULT_ALIAS")
 if path!=""{
  if endpoint!=""||name!=""||tokenFile!=""||vaultAlias!=""{
   return nil,errors.New("legacy model and capability routes are mutually exclusive")
  }
  if !filepath.IsAbs(path){return nil,errors.New("absolute model route configuration required")}
  bytesOnDisk,err:=os.ReadFile(path)
  if err!=nil{return nil,err}
  if len(bytesOnDisk)>32<<10{return nil,errors.New("model route configuration exceeds limit")}
  var input struct{Routes []modelRouteSpec `json:"routes"`}
  dec:=json.NewDecoder(bytes.NewReader(bytesOnDisk))
  dec.DisallowUnknownFields()
  if err:=dec.Decode(&input);err!=nil{return nil,err}
  var trailing any
  if err:=dec.Decode(&trailing);err!=io.EOF{return nil,errors.New("model route configuration contains extra JSON")}
  routes:=make([]agentflow.ModelRoute,0,len(input.Routes))
  for _,spec:=range input.Routes{
   if spec.VaultAlias!=""&&spec.TokenFile!=""{
    return nil,errors.New("model route has conflicting credential authorities")
   }
   var source func()(string,error)
   if spec.VaultAlias!=""{
    if vault==nil{return nil,errors.New("model route vault unavailable")}
    var err error
    source,err=vault.TokenSource(spec.VaultAlias)
    if err!=nil{return nil,err}
   }
   token:=""
   if spec.TokenFile!=""{
    if !filepath.IsAbs(spec.TokenFile){return nil,errors.New("model token path must be absolute")}
    info,err:=os.Lstat(spec.TokenFile)
    if err!=nil||!info.Mode().IsRegular()||info.Mode().Perm()&0077!=0{
     return nil,errors.New("model token file must be private")
    }
    token,err=secret(spec.TokenFile)
    if err!=nil{return nil,err}
   }
   routes=append(routes,agentflow.ModelRoute{
    Name:spec.Name,Purpose:spec.Purpose,Endpoint:spec.Endpoint,Model:spec.Model,
    LocalOnly:spec.LocalOnly,Token:token,TokenSource:source,
   })
  }
  return agentflow.NewModelRouter(routes)
 }
 if endpoint==""{
  if name!=""||tokenFile!=""||vaultAlias!=""{return nil,errors.New("model name and token require an endpoint")}
  return nil,nil
 }
 if tokenFile!=""&&vaultAlias!=""{return nil,errors.New("model has conflicting credential authorities")}
 var source func()(string,error)
 if vaultAlias!=""{
  if vault==nil{return nil,errors.New("model vault unavailable")}
  var err error
  source,err=vault.TokenSource(vaultAlias)
  if err!=nil{return nil,err}
 }
 token:=""
 if tokenFile!=""{
  var err error
  token,err=secret(tokenFile)
  if err!=nil{return nil,err}
 }
 author,err:=agentflow.NewModelAuthor(endpoint,name,token)
 if err!=nil{return nil,err}
 author.TokenSource=source
 return author,nil
}
