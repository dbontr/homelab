package security

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "sync"
 "time"

 "github.com/dbontr/homelab-new/system/canonicaljson"
)

// CredentialUse authorizes one invocation without transferring stored secrets
// to an MCP gateway, model context, or worker request.
type CredentialUse struct {
 ID string `json:"id"`
 Provider string `json:"provider"`
 Operation string `json:"operation"`
 Target string `json:"target"`
 ExpiresAt time.Time `json:"expires_at"`
}

type credentialLease struct {
 CredentialUse
 principal string
 handle string
 digest string
 version string
}

type ScopedCredentialBroker struct {
 mu sync.Mutex
 vault *Vault
 sessions *AdminSessions
 leases map[string]credentialLease
 now func()time.Time
}

func NewScopedCredentialBroker(vault *Vault,sessions *AdminSessions)(*ScopedCredentialBroker,error){
 if vault==nil||sessions==nil{return nil,errors.New("vault and authorization service are required")}
 return &ScopedCredentialBroker{vault:vault,sessions:sessions,leases:map[string]credentialLease{},now:time.Now},nil
}

func credentialDigest(args json.RawMessage)(string,error){
 canonical,_,err:=canonicaljson.Object(args)
 if err!=nil{return "",err}
 sum:=sha256.Sum256(canonical)
 return hex.EncodeToString(sum[:]),nil
}

// Prepare requires a verified owner principal. It returns only a short-lived
// opaque identifier; stored handles and ciphertext are not disclosed.
func(b *ScopedCredentialBroker) Prepare(principal,handle,provider,operation,target string,args json.RawMessage,ttl time.Duration)(CredentialUse,error){
 if principal==""||handle==""||provider==""||operation==""||target==""||
  !allowedIdentifier.MatchString(operation)||!allowedIdentifier.MatchString(target)||
  ttl<=0||ttl>2*time.Minute {
  return CredentialUse{},errors.New("invalid credential operation scope")
 }
 if !b.sessions.StatusFor(principal).Active{return CredentialUse{},errors.New("verified owner session required")}
 digest,err:=credentialDigest(args);if err!=nil{return CredentialUse{},err}
 version,err:=b.vault.Version(handle,provider)
 if err!=nil{return CredentialUse{},errors.New("credential handle not found")}
 id,err:=randomURLToken(24);if err!=nil{return CredentialUse{},err}
 b.mu.Lock();defer b.mu.Unlock()
 now:=b.now().UTC()
 for key,lease:=range b.leases{if !now.Before(lease.ExpiresAt){delete(b.leases,key)}}
 if len(b.leases)>=64{return CredentialUse{},errors.New("too many pending credential uses")}
 public:=CredentialUse{ID:id,Provider:provider,Operation:operation,Target:target,ExpiresAt:now.Add(ttl)}
 b.leases[id]=credentialLease{CredentialUse:public,principal:principal,handle:handle,digest:digest,version:version}
 return public,nil
}

// Consume authorizes the exact scope at the real point of credential use.
// It consumes the lease before invoking the trusted caller's operation.
func(b *ScopedCredentialBroker) Consume(_ context.Context,principal,id,operation,target string,args json.RawMessage,use func([]byte)error)(Authorization,error){
 if use==nil{return Authorization{},errors.New("credential operation missing")}
 digest,err:=credentialDigest(args);if err!=nil{return Authorization{},err}
 b.mu.Lock()
 lease,ok:=b.leases[id]
 now:=b.now().UTC()
 if !ok||!now.Before(lease.ExpiresAt)||principal==""||lease.principal!=principal||
  lease.Operation!=operation||lease.Target!=target||lease.digest!=digest{
  b.mu.Unlock()
  return Authorization{Reason:"credential capability unavailable"},nil
 }
 // Confirmation is an independent, operation-bound check inside securityd.
 decision:=b.sessions.AuthorizeFor(principal,"platform-gateway",operation,target,args)
 if !decision.Allowed{
  b.mu.Unlock()
  return decision,nil
 }
 delete(b.leases,id)
 b.mu.Unlock()
 if err:=b.vault.WithCredentialVersion(lease.handle,lease.Provider,lease.version,use);err!=nil{
  return Authorization{Reason:"credential use failed"},err
 }
 return Authorization{Allowed:true},nil
}

func(b *ScopedCredentialBroker) Revoke(id string)bool{
 b.mu.Lock();defer b.mu.Unlock()
 if _,ok:=b.leases[id];!ok{return false}
 delete(b.leases,id)
 return true
}

func(b *ScopedCredentialBroker) RevokeAll(){
 b.mu.Lock();defer b.mu.Unlock()
 b.leases=map[string]credentialLease{}
}
