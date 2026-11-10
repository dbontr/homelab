// homelab-securityd provides owner-controlled privileged sessions over Unix sockets.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/dbontr/homelab-new/system/statelease"

	"github.com/dbontr/homelab-new/system/security"
)

func readRequiredFile(name string) ([]byte,error) {
 return readConfiguredFile(name,false)
}

func readPrivateCredential(name string) ([]byte,error) {
 return readConfiguredFile(name,true)
}

// readConfiguredFile permits public, immutable policy material but requires
// owner-only file permissions for password, TOTP and vault credentials.
func readConfiguredFile(name string, private bool) ([]byte,error) {
 path:=os.Getenv(name)
 if !filepath.IsAbs(path){return nil,errors.New("required protected file path missing")}
 file,err:=os.OpenFile(path,os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC,0)
 if err!=nil{return nil,errors.New("configured file cannot be opened")}
 defer file.Close()
 info,err:=file.Stat()
 if err!=nil||!info.Mode().IsRegular(){return nil,errors.New("configured file must be regular")}
 mode:=info.Mode().Perm()
 if (private&&mode&0077!=0)||(!private&&mode&0022!=0){
  return nil,errors.New("configured file permissions are unsafe")
 }
 const maxConfiguredFileBytes=1<<20
 raw,err:=io.ReadAll(io.LimitReader(file,maxConfiguredFileBytes+1))
 if err!=nil||len(raw)>maxConfiguredFileBytes{return nil,errors.New("configured file exceeds size limit")}
 return raw,nil
}

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)
 lease,err:=statelease.Acquire("/var/lib/homelab-securityd","securityd")
 if err!=nil{log.Fatal("cannot acquire exclusive security state ownership: ",err)}
 defer lease.Close()
	verifierBytes, err := readPrivateCredential("HOMELAB_SECURITY_PASSWORD_FILE")
	if err != nil {
		log.Fatal("cannot load owner password verifier: ", err)
	}
	totpBytes, err := readPrivateCredential("HOMELAB_SECURITY_TOTP_FILE")
	if err != nil {
		log.Fatal("cannot load TOTP configuration: ", err)
	}
	policyBytes, err := readRequiredFile("HOMELAB_SECURITY_POLICY_FILE")
	if err != nil {
		log.Fatal("cannot load operation policy: ", err)
	}
	var verifier security.PassphraseRecord
	if err := json.Unmarshal(verifierBytes, &verifier); err != nil {
		log.Fatal("invalid password verifier")
	}
	var policies []security.OperationPolicy
	if err := json.Unmarshal(policyBytes, &policies); err != nil {
		log.Fatal("invalid security policy")
	}
	manager, err := security.NewAdminSessions(security.SessionConfig{
		Owner: os.Getenv("HOMELAB_SECURITY_OWNER"), Client: "platform-gateway",
		Verifier: verifier, TOTPSecret: strings.TrimSpace(string(totpBytes)),
		StateFile: "/var/lib/homelab-securityd/authenticator.json", Policies: policies,
	})
	if err != nil {
		log.Fatal("cannot initialize security service: ", err)
	}
 if os.Getenv("HOMELAB_SECURITY_VAULT_KEY_FILE")!=""{
  key,err:=readPrivateCredential("HOMELAB_SECURITY_VAULT_KEY_FILE")
  if err!=nil||len(key)!=32{log.Fatal("invalid credential vault key")}
  vault,err:=security.OpenVault("/var/lib/homelab-securityd/credentials.json",key)
  for i:=range key{key[i]=0}
  if err!=nil{log.Fatal("cannot open protected credential vault: ",err)}
  if err:=manager.SetVault(vault);err!=nil{log.Fatal(err)}
 }
 if raw:=os.Getenv("HOMELAB_SECURITY_OAUTH_PROVIDERS");raw!=""{
  if os.Getenv("HOMELAB_SECURITY_VAULT_KEY_FILE")==""{
   log.Fatal("OAuth providers require the independent credential vault")
  }
  var providers []security.Provider
  if err:=json.Unmarshal([]byte(raw),&providers);err!=nil||len(providers)==0{
   log.Fatal("invalid configured OAuth provider registry")
  }
  broker,err:=security.NewOAuthBroker(providers)
  if err!=nil{log.Fatal("invalid OAuth provider policy: ",err)}
  if err:=manager.SetOAuthBroker(broker,nil);err!=nil{log.Fatal(err)}
 }
 if raw:=os.Getenv("HOMELAB_SECURITY_SERVICE_CREDENTIAL_GRANTS");raw!=""{
  var grants []security.ServiceCredentialGrant
  if err:=json.Unmarshal([]byte(raw),&grants);err!=nil||len(grants)==0{
   log.Fatal("invalid service credential grants")
  }
  if err:=manager.SetServiceCredentialGrants(grants);err!=nil{
   log.Fatal("invalid service credential policy: ",err)
  }
 }
 var verifier *security.AccessVerifier
 gatewayAudience:=os.Getenv("HOMELAB_SECURITY_ACCESS_GATEWAY_AUD")
 if gatewayAudience!="" {
  jwks,err:=readRequiredFile("HOMELAB_SECURITY_ACCESS_JWKS_FILE")
  if err!=nil{log.Fatal("Access JWKS unavailable: ",err)}
  email,err:=readPrivateCredential("HOMELAB_SECURITY_ACCESS_OWNER_FILE")
  if err!=nil{log.Fatal("owner identity unavailable: ",err)}
  verifier,err=security.NewAccessVerifier(os.Getenv("HOMELAB_SECURITY_ACCESS_ISSUER"),strings.TrimSpace(string(email)),jwks)
  if err!=nil{log.Fatal("invalid verified owner identity configuration: ",err)}
  if err:=manager.SetAccessVerifier(verifier,gatewayAudience);err!=nil{log.Fatal(err)}
 }
	group, err := user.LookupGroup("homelab-security-client")
	if err != nil {
		log.Fatal("security client group is unavailable: ", err)
	}
	gid, err := strconv.Atoi(group.Gid)
	if err != nil || gid <= 0 {
		log.Fatal("invalid security client group identifier")
	}
	dir := "/run/homelab-securityd"
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	failures := make(chan error, 4)
	go func() {
		failures <- manager.ServeSockets(ctx, filepath.Join(dir, "owner.sock"), filepath.Join(dir, "client.sock"), gid)
	}()
 if managerOAuth:=os.Getenv("HOMELAB_SECURITY_OAUTH_PROVIDERS");managerOAuth!=""{
  // Device-code-only providers do not require a TCP callback listener.
  // The callback is enabled only for an explicitly configured browser flow.
  var providers []security.Provider
  if err:=json.Unmarshal([]byte(managerOAuth),&providers);err!=nil{log.Fatal("invalid OAuth providers")}
  for _,provider:=range providers{
   if provider.RedirectURL!=""{
    go func(){failures<-manager.ServeOAuthCallback(ctx)}()
    break
   }
  }
 }
	if origin := os.Getenv("HOMELAB_SECURITY_BROWSER_ORIGIN"); origin != "" {
		if verifier==nil{log.Fatal("browser login requires independent Access identity verification")}
  browserAudience:=os.Getenv("HOMELAB_SECURITY_ACCESS_BROWSER_AUD")
  if browserAudience==""||browserAudience==gatewayAudience{log.Fatal("browser login needs a dedicated audience")}
  portal, err := security.NewBrowserPortal(manager, origin,func(assertion string)(string,error){
   return verifier.Verify(assertion,browserAudience)
  })
		if err != nil {
			log.Fatal("invalid protected browser origin: ", err)
		}
		group, err := user.LookupGroup("homelab-security-browser")
		if err != nil {
			log.Fatal("browser socket group unavailable: ", err)
		}
		webGroupID, err := strconv.Atoi(group.Gid)
		if err != nil || webGroupID <= 0 {
			log.Fatal("invalid browser socket group")
		}
		go func() {
			failures <- portal.ServeBrowserSocket(ctx, "/run/homelab-security-browser/browser.sock", webGroupID)
		}()
	}
 if listen:=os.Getenv("HOMELAB_SECURITY_REMOTE_LISTEN");listen!=""{
  cert,err:=tls.LoadX509KeyPair(os.Getenv("HOMELAB_SECURITY_REMOTE_CERT"),os.Getenv("HOMELAB_SECURITY_REMOTE_KEY"))
  if err!=nil{log.Fatal("remote security certificate unavailable: ",err)}
  ca,err:=readRequiredFile("HOMELAB_SECURITY_REMOTE_CLIENT_CA")
  if err!=nil{log.Fatal("remote security client trust unavailable: ",err)}
  roots:=x509.NewCertPool()
  if !roots.AppendCertsFromPEM(ca){log.Fatal("invalid remote security client trust")}
  var clients []string
  if err:=json.Unmarshal([]byte(os.Getenv("HOMELAB_SECURITY_REMOTE_CLIENT_IDENTITIES")),&clients);err!=nil||len(clients)==0{
   log.Fatal("invalid trusted executor identity list")
  }
  go func(){failures<-manager.ServeAuthorizationTLS(ctx,listen,cert,roots,clients)}()
 }
	log.Print("independent owner authentication sockets starting")
	err = <-failures
	stop()
	if err != nil {
		log.Fatal("security sockets stopped: ", err)
	}
}
