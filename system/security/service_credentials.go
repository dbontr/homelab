package security

import (
 "bytes"
 "errors"
 "net/http"
 "regexp"
 "strings"
)

// ServiceCredentialGrant lets a named, mTLS-verified service read one
// credential from the owner vault. The handle and policy are trusted Nix
// configuration, never parameters accepted from MCP or a remote request.
type ServiceCredentialGrant struct {
 ClientDNS string `json:"client_dns"`
 Alias string `json:"alias"`
 Provider string `json:"provider"`
 Handle string `json:"handle"`
}

var serviceCredentialName=regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)
var serviceCredentialHandle=regexp.MustCompile(`^[A-Za-z0-9_-]{32}$`)

func serviceCredentialKey(identity,alias string)string{return identity+"\x00"+alias}

// SetServiceCredentialGrants registers an immutable, explicit service identity
// to credential binding. A revoked or rotated vault record is used on each
// request; the daemon does not retain a plaintext credential cache.
func(a *AdminSessions)SetServiceCredentialGrants(grants []ServiceCredentialGrant)error{
 if a==nil||len(grants)>128{return errors.New("invalid service credential policy")}
 a.mu.Lock()
 defer a.mu.Unlock()
 if a.vault==nil{return errors.New("service credentials require the independent vault")}
 if a.serviceCredentialGrants!=nil{return errors.New("service credential policy is already configured")}
 policy:=map[string]ServiceCredentialGrant{}
 for _,grant:=range grants{
  if grant.ClientDNS==""||len(grant.ClientDNS)>253||
   strings.ContainsAny(grant.ClientDNS,"/?#:@\\ \t\r\n\x00")||
   !serviceCredentialName.MatchString(grant.Alias)||
   !serviceCredentialName.MatchString(grant.Provider)||
   !serviceCredentialHandle.MatchString(grant.Handle){
   return errors.New("invalid service credential grant")
  }
  key:=serviceCredentialKey(grant.ClientDNS,grant.Alias)
  if _,exists:=policy[key];exists{return errors.New("duplicate service credential grant")}
  if _,err:=a.vault.Version(grant.Handle,grant.Provider);err!=nil{
   return errors.New("service credential handle does not exist in the owner vault")
  }
  policy[key]=grant
 }
 a.serviceCredentialGrants=policy
 return nil
}

func verifiedServicePeer(request *http.Request)(string,bool){
 if request.TLS==nil||len(request.TLS.VerifiedChains)==0||
  len(request.TLS.PeerCertificates)==0{return "",false}
 peer:=request.TLS.PeerCertificates[0]
 if peer==nil||len(peer.DNSNames)!=1{return "",false}
 dns:=peer.DNSNames[0]
 if peer.VerifyHostname(dns)!=nil{return "",false}
 // The authorization listener checks the peer's cert chain and configured
 // identity allowlist at connection establishment.
 return dns,true
}

// ServiceCredentialHandler is mounted solely on the private mTLS listener.
// It is never registered on the owner, browser or MCP gateway handlers.
func(a *AdminSessions)ServiceCredentialHandler()http.Handler{
 return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  w.Header().Set("Cache-Control","no-store")
  w.Header().Set("Pragma","no-cache")
  w.Header().Set("X-Content-Type-Options","nosniff")
  w.Header().Set("Referrer-Policy","no-referrer")
  alias:=r.PathValue("alias")
  identity,verified:=verifiedServicePeer(r)
  if r.Method!=http.MethodGet||!verified||!serviceCredentialName.MatchString(alias){
   http.Error(w,"service credential access denied",http.StatusForbidden)
   return
  }
  a.mu.Lock()
  grant,found:=a.serviceCredentialGrants[serviceCredentialKey(identity,alias)]
  vault:=a.vault
  a.mu.Unlock()
  if !found||vault==nil{
   http.Error(w,"service credential access denied",http.StatusForbidden)
   return
  }
  var secret []byte
  err:=vault.WithCredential(grant.Handle,grant.Provider,func(raw []byte)error{
   if len(raw)==0||len(raw)>32768{return errors.New("invalid protected credential")}
   secret=bytes.Clone(raw)
   return nil
  })
  if err!=nil{
   http.Error(w,"service credential unavailable",http.StatusForbidden)
   return
  }
  defer func(){for i:=range secret{secret[i]=0}}()
  w.Header().Set("Content-Type","application/octet-stream")
  w.WriteHeader(http.StatusOK)
  _,_=w.Write(secret)
 })
}

