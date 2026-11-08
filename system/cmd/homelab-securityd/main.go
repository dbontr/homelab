// homelab-securityd provides owner-controlled privileged sessions over Unix sockets.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/dbontr/homelab-new/system/security"
)

func readRequiredFile(name string) ([]byte, error) {
	path := os.Getenv(name)
	if !filepath.IsAbs(path) {
		return nil, errors.New("required protected credential path missing")
	}
	return os.ReadFile(path)
}

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)
	verifierBytes, err := readRequiredFile("HOMELAB_SECURITY_PASSWORD_FILE")
	if err != nil {
		log.Fatal("cannot load owner password verifier: ", err)
	}
	totpBytes, err := readRequiredFile("HOMELAB_SECURITY_TOTP_FILE")
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
  key,err:=readRequiredFile("HOMELAB_SECURITY_VAULT_KEY_FILE")
  if err!=nil||len(key)!=32{log.Fatal("invalid credential vault key")}
  vault,err:=security.OpenVault("/var/lib/homelab-securityd/credentials.json",key)
  for i:=range key{key[i]=0}
  if err!=nil{log.Fatal("cannot open protected credential vault: ",err)}
  if err:=manager.SetVault(vault);err!=nil{log.Fatal(err)}
 }
 var verifier *security.AccessVerifier
 gatewayAudience:=os.Getenv("HOMELAB_SECURITY_ACCESS_GATEWAY_AUD")
 if gatewayAudience!="" {
  jwks,err:=readRequiredFile("HOMELAB_SECURITY_ACCESS_JWKS_FILE")
  if err!=nil{log.Fatal("Access JWKS unavailable: ",err)}
  email,err:=readRequiredFile("HOMELAB_SECURITY_ACCESS_OWNER_FILE")
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
	failures := make(chan error, 2)
	go func() {
		failures <- manager.ServeSockets(ctx, filepath.Join(dir, "owner.sock"), filepath.Join(dir, "client.sock"), gid)
	}()
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
	log.Print("independent owner authentication sockets starting")
	err = <-failures
	stop()
	if err != nil {
		log.Fatal("security sockets stopped: ", err)
	}
}
