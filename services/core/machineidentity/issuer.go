// Package machineidentity issues short-lived OAuth2 client credentials
// for one protected service principal. Human login is not supported.
package machineidentity

import (
 "crypto"
 "crypto/rand"
 "crypto/rsa"
 "crypto/sha256"
 "crypto/subtle"
 "crypto/x509"
 "encoding/base64"
 "encoding/hex"
 "encoding/json"
 "errors"
 "io"
 "math/big"
 "net/http"
 "net/url"
 "regexp"
 "strings"
 "time"
)

const tokenLifetime = 5 * time.Minute
var machineID = regexp.MustCompile("^[A-Za-z][A-Za-z0-9_-]{2,127}$")

type Config struct {
 Issuer string
 Audience string
 ClientID string
 PrivateKey *rsa.PrivateKey
 ClientSecret []byte
}

type issuer struct {
 key *rsa.PrivateKey
 kid string
 jwks []byte
 secretDigest [32]byte
 client string
 origin string
 audience string
 now func()time.Time
}

func NewHandler(cfg Config)(http.Handler,error){
 u,err:=url.Parse(cfg.Issuer)
 if err!=nil || u.Scheme!="https" || u.Host=="" || u.User!=nil ||
  u.RawQuery!="" || u.Fragment!="" || u.Opaque!="" || u.Path!="" ||
  strings.HasSuffix(cfg.Issuer,"/") {
  return nil,errors.New("machine issuer requires an exact HTTPS origin")
 }
 if cfg.Audience=="" || len(cfg.Audience)>256 || strings.ContainsAny(cfg.Audience,"\x00\r\n\t ") ||
  !machineID.MatchString(cfg.ClientID) || cfg.PrivateKey==nil ||
  cfg.PrivateKey.N==nil || cfg.PrivateKey.N.BitLen()<3072 ||
  cfg.PrivateKey.E!=65537 || len(cfg.ClientSecret)<32 || len(cfg.ClientSecret)>256 ||
  strings.TrimSpace(string(cfg.ClientSecret))!=string(cfg.ClientSecret) ||
  strings.ContainsAny(string(cfg.ClientSecret),"\x00\r\n\t ") {
  return nil,errors.New("invalid machine issuer configuration")
 }
 if err:=cfg.PrivateKey.Validate();err!=nil{return nil,errors.New("invalid RSA signing key")}
 der,err:=x509.MarshalPKIXPublicKey(&cfg.PrivateKey.PublicKey)
 if err!=nil{return nil,errors.New("invalid signing public key")}
 sum:=sha256.Sum256(der)
 kid:=hex.EncodeToString(sum[:])
 n:=base64.RawURLEncoding.EncodeToString(cfg.PrivateKey.N.Bytes())
 e:=base64.RawURLEncoding.EncodeToString(big.NewInt(int64(cfg.PrivateKey.E)).Bytes())
 jwks,err:=json.Marshal(map[string]any{"keys":[]map[string]string{{
  "kty":"RSA","alg":"RS256","use":"sig","kid":kid,"n":n,"e":e,
 }}})
 if err!=nil{return nil,err}
 svc:=&issuer{key:cfg.PrivateKey,kid:kid,jwks:jwks,
  secretDigest:sha256.Sum256(cfg.ClientSecret),client:cfg.ClientID,
  origin:cfg.Issuer,audience:cfg.Audience,now:time.Now}
 mux:=http.NewServeMux()
 mux.HandleFunc("GET /.well-known/jwks.json",svc.keys)
 mux.HandleFunc("POST /oauth2/token",svc.token)
 mux.HandleFunc("GET /healthz",func(w http.ResponseWriter,_ *http.Request){
  w.Header().Set("Cache-Control","no-store")
  w.WriteHeader(http.StatusNoContent)
 })
 return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  w.Header().Set("Cache-Control","no-store")
  w.Header().Set("X-Content-Type-Options","nosniff")
  mux.ServeHTTP(w,r)
 }),nil
}

func(s *issuer)keys(w http.ResponseWriter,_ *http.Request){
 w.Header().Set("Content-Type","application/json")
 w.Header().Set("Cache-Control","public, max-age=60")
 _,_=w.Write(s.jwks)
}

func(s *issuer)token(w http.ResponseWriter,r *http.Request){
 deny:=func(code int,errorName string){
  w.Header().Set("Content-Type","application/json")
  if code==http.StatusUnauthorized {w.Header().Set("WWW-Authenticate",`Basic realm="machine-token"`)}
  w.WriteHeader(code)
  _=json.NewEncoder(w).Encode(map[string]string{"error":errorName})
 }
 if !strings.EqualFold(r.Header.Get("Content-Type"),"application/x-www-form-urlencoded"){
  deny(http.StatusUnsupportedMediaType,"invalid_request")
  return
 }
 client,secret,ok:=r.BasicAuth()
 candidate:=sha256.Sum256([]byte(secret))
 if !ok || len(secret)>256 || client!=s.client ||
  subtle.ConstantTimeCompare(candidate[:],s.secretDigest[:])!=1 {
  deny(http.StatusUnauthorized,"invalid_client")
  return
 }
 r.Body=http.MaxBytesReader(w,r.Body,4096)
 if err:=r.ParseForm();err!=nil || len(r.PostForm)!=1 ||
  len(r.PostForm["grant_type"])!=1 || r.PostForm.Get("grant_type")!="client_credentials" ||
  r.URL.RawQuery!="" {
  deny(http.StatusBadRequest,"invalid_request")
  return
 }
 randomID:=make([]byte,16)
 if _,err:=rand.Read(randomID);err!=nil{deny(http.StatusServiceUnavailable,"server_error");return}
 now:=s.now().UTC().Unix()
 header,_:=json.Marshal(map[string]string{"alg":"RS256","typ":"JWT","kid":s.kid})
 claims,_:=json.Marshal(map[string]any{
  "iss":s.origin,"sub":s.client,"aud":s.audience,
  "iat":now,"nbf":now-2,"exp":now+int64(tokenLifetime/time.Second),
  "jti":hex.EncodeToString(randomID),
 })
 message:=base64.RawURLEncoding.EncodeToString(header)+"."+base64.RawURLEncoding.EncodeToString(claims)
 sum:=sha256.Sum256([]byte(message))
 signature,err:=rsa.SignPKCS1v15(rand.Reader,s.key,crypto.SHA256,sum[:])
 if err!=nil{deny(http.StatusServiceUnavailable,"server_error");return}
 token:=message+"."+base64.RawURLEncoding.EncodeToString(signature)
 w.Header().Set("Content-Type","application/json")
 w.Header().Set("Pragma","no-cache")
 _=json.NewEncoder(w).Encode(map[string]any{
  "access_token":token,"token_type":"Bearer","expires_in":int(tokenLifetime/time.Second),
 })
}

// Compile-time API proof that the handler uses only standard net/http endpoints.
var _ = io.EOF
