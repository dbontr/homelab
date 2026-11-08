package privileged

import (
 "context"
 "crypto/tls"
 "crypto/x509"
 "encoding/json"
 "errors"
 "net"
 "net/http"
 "time"

 "github.com/dbontr/homelab-new/system/release"
 "github.com/dbontr/homelab-new/system/security"
)

// RemoteHandler exposes typed executor operations and an independently
// authorized, read-only local release-journal observation. TLS client
// authentication is enforced by ServeMutualTLS.
func(e *Executor)RemoteHandler()http.Handler{
 mux:=http.NewServeMux()
 e.registerReleaseStage(mux)
 mux.Handle("POST /v1/execute",e.Handler())
 mux.HandleFunc("GET /v1/release/status",func(w http.ResponseWriter,r *http.Request){
  w.Header().Set("Cache-Control","no-store")
  w.Header().Set("X-Content-Type-Options","nosniff")
  assertion:=r.Header.Get("Cf-Access-Jwt-Assertion")
  if assertion==""{http.Error(w,"owner assertion required",http.StatusForbidden);return}
  if !e.cfg.AllowRelease{http.Error(w,"release administration disabled",http.StatusForbidden);return}
  source,ok:=e.cfg.Releases.(interface{Status()(release.Journal,error)})
  if !ok{http.Error(w,"release observation unavailable",http.StatusServiceUnavailable);return}
  args,err:=json.Marshal(struct{Host string `json:"host"`}{e.cfg.Host})
  if err!=nil{http.Error(w,"invalid host",http.StatusInternalServerError);return}
  decision,err:=e.cfg.Auth.Authorize(security.WithAccessAssertion(r.Context(),assertion),
   "infrastructure__release_status",e.cfg.Host,args)
  if err!=nil||!decision.Allowed{http.Error(w,"independent authorization denied",http.StatusForbidden);return}
  journal,err:=source.Status()
  if err!=nil||!releaseJournalID(journal.ID){http.Error(w,"release journal unavailable",http.StatusServiceUnavailable);return}
  w.Header().Set("Content-Type","application/json")
  _=json.NewEncoder(w).Encode(release.ReleaseObservation{ID:journal.ID,Phase:journal.Phase})
 })
 // Defense in depth: this handler is never valid on an ordinary HTTP
 // listener or a TLS listener that did not verify the coordinator client.
 return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.TLS==nil||len(r.TLS.VerifiedChains)==0||len(r.TLS.PeerCertificates)==0{
   http.Error(w,"verified executor client certificate required",http.StatusForbidden)
   return
  }
  mux.ServeHTTP(w,r)
 })
}

func releaseJournalID(id string)bool{
 if len(id)!=48{return false}
 for _,ch:=range id{
  if (ch<'0'||ch>'9')&&(ch<'a'||ch>'f'){return false}
 }
 return true
}

// ServeMutualTLS is bound only on an operator-selected private interface.
// A verified coordinator client certificate must match the configured DNS SAN.
func(e *Executor)ServeMutualTLS(ctx context.Context,address string,cert tls.Certificate,clientCA *x509.CertPool,coordinatorDNS string)error{
 if e==nil||clientCA==nil||coordinatorDNS==""||len(cert.Certificate)==0||cert.PrivateKey==nil{
  return errors.New("remote executor requires TLS identity and client CA")
 }
 host,_,err:=net.SplitHostPort(address)
 if err!=nil{return errors.New("invalid remote executor listener")}
 ip:=net.ParseIP(host)
 if ip==nil||!(ip.IsPrivate()||ip.IsLoopback()){return errors.New("remote executor must bind an explicit private IP")}
 listener,err:=net.Listen("tcp",address);if err!=nil{return err}
 defer listener.Close()
 configuration:=&tls.Config{
  MinVersion:tls.VersionTLS13,ClientAuth:tls.RequireAndVerifyClientCert,ClientCAs:clientCA,
  Certificates:[]tls.Certificate{cert},
  VerifyConnection:func(state tls.ConnectionState)error{
   if len(state.VerifiedChains)==0||len(state.PeerCertificates)==0{return errors.New("unverified coordinator certificate")}
   return state.PeerCertificates[0].VerifyHostname(coordinatorDNS)
  },
 }
 server:=&http.Server{Handler:e.RemoteHandler(),ReadHeaderTimeout:5*time.Second,ReadTimeout:15*time.Second,WriteTimeout:2*time.Hour}
 stopped:=make(chan struct{})
 go func(){select{case <-ctx.Done():_=server.Close();case <-stopped:}}()
 err=server.Serve(tls.NewListener(listener,configuration));close(stopped)
 if errors.Is(err,http.ErrServerClosed){return nil}
 return err
}
