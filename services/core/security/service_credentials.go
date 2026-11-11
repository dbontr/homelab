package security

import (
 "bytes"
 "crypto/tls"
 "errors"
 "net"
 "net/http"
 "regexp"
 "strings"
 "time"
)

// ServiceCredentialGrant lets a named, mTLS-verified service read one
// credential from the owner vault. The handle and policy are trusted Nix
// configuration, never parameters accepted from MCP or a remote request.
type ServiceCredentialGrant struct {
 ClientDNS string `json:"client_dns"`
 Alias string `json:"alias"`
 Provider string `json:"provider"`
 Handle string `json:"handle"`
 Revision string `json:"revision,omitempty"`
 ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

var serviceCredentialName=regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)
var serviceCredentialHandle=regexp.MustCompile(`^[A-Za-z0-9_-]{32}$`)

// ValidServiceDNS accepts one canonical ASCII host identity, never a wildcard,
// URL, or DNS name with an ambiguous spelling.
func ValidServiceDNS(name string) bool {
 if name=="" || len(name)>253 || net.ParseIP(name)!=nil { return false }
 for _,label:=range strings.Split(name,".") {
  if len(label)==0 || len(label)>63 { return false }
  for i:=0;i<len(label);i++ {
   c:=label[i]
   alpha:=c>='a' && c<='z'
   digit:=c>='0' && c<='9'
   if !alpha && !digit && (c!='-' || i==0 || i==len(label)-1) { return false }
  }
 }
 return true
}

func serviceCredentialKey(identity,alias string)string{return identity+"\x00"+alias}

// SetServiceCredentialGrants registers an immutable, explicit service identity
// to credential binding. The grant captures the current encrypted-record
// revision unless an exact revision was configured. Rotation or deletion
// invalidates that binding; no plaintext credential is cached.
func(a *AdminSessions)SetServiceCredentialGrants(grants []ServiceCredentialGrant)error{
 if a==nil||len(grants)>128{return errors.New("invalid service credential policy")}
 a.mu.Lock()
 defer a.mu.Unlock()
 if a.vault==nil{return errors.New("service credentials require the independent vault")}
 if a.serviceCredentialGrants!=nil{return errors.New("service credential policy is already configured")}
 policy:=map[string]ServiceCredentialGrant{}
 for _,grant:=range grants{
  if !ValidServiceDNS(grant.ClientDNS)||
   !serviceCredentialName.MatchString(grant.Alias)||
   !serviceCredentialName.MatchString(grant.Provider)||
   !serviceCredentialHandle.MatchString(grant.Handle){
   return errors.New("invalid service credential grant")
  }
  key:=serviceCredentialKey(grant.ClientDNS,grant.Alias)
  if _,exists:=policy[key];exists{return errors.New("duplicate service credential grant")}
  revision,err:=a.vault.Version(grant.Handle,grant.Provider)
  if err!=nil{
   return errors.New("service credential handle does not exist in the owner vault")
  }
  if grant.Revision!="" {
   if len(grant.Revision)!=64 || strings.Trim(grant.Revision,"0123456789abcdef")!="" || grant.Revision!=revision{
    return errors.New("service credential grant revision is invalid or stale")
   }
  } else {
   grant.Revision=revision
  }
  if grant.ExpiresAt!=nil && !grant.ExpiresAt.After(time.Now().UTC()){
   return errors.New("service credential grant has expired")
  }
  policy[key]=grant
 }
 a.serviceCredentialGrants=policy
 return nil
}

func verifiedServiceDNS(state *tls.ConnectionState)(string,bool){
 if state==nil||len(state.VerifiedChains)==0||
  len(state.PeerCertificates)==0{return "",false}
 peer:=state.PeerCertificates[0]
 if peer==nil||len(peer.DNSNames)!=1||len(peer.IPAddresses)!=0||
  len(peer.EmailAddresses)!=0||len(peer.URIs)!=0{return "",false}
 dns:=peer.DNSNames[0]
 if !ValidServiceDNS(dns)||peer.VerifyHostname(dns)!=nil{return "",false}
 return dns,true
}

func verifiedServicePeer(request *http.Request)(string,bool){
 // The authorization listener checks the peer's cert chain and configured
 // identity allowlist at connection establishment.
 return verifiedServiceDNS(request.TLS)
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
  if !found||vault==nil||
   (grant.ExpiresAt!=nil&&!time.Now().UTC().Before(*grant.ExpiresAt)){
   http.Error(w,"service credential access denied",http.StatusForbidden)
   return
  }
  var secret []byte
  err:=vault.WithCredentialVersion(grant.Handle,grant.Provider,grant.Revision,func(raw []byte)error{
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

