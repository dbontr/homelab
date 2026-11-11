// Offline verification of the Go authority's compatibility with a state copy.
// This command never opens security sockets or writes authentication records.
package main

import (
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "strings"
    "syscall"

    "github.com/dbontr/homelab-new/system/security"
)

func read(name string, private bool)([]byte,error){
    path:=os.Getenv(name)
    if path==""||!filepath.IsAbs(path)||filepath.Clean(path)!=path{
        return nil,errors.New("configured credential path must be absolute")
    }
    file,err:=os.OpenFile(path,os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC,0)
    if err!=nil{return nil,errors.New("configured credential unavailable")}
    defer file.Close()
    info,err:=file.Stat()
    if err!=nil||!info.Mode().IsRegular()||info.Size()<1||info.Size()>1<<20{
        return nil,errors.New("invalid protected configuration file")
    }
    mode:=info.Mode().Perm()
    inode,ok:=info.Sys().(*syscall.Stat_t)
    if !ok||inode.Nlink!=1||
        (private&&(mode&0077!=0||inode.Uid!=uint32(os.Geteuid())))||
        (!private&&mode&0022!=0){
        return nil,errors.New("configured file has unsafe ownership or permissions")
    }
    data,err:=io.ReadAll(io.LimitReader(file,(1<<20)+1))
    if err!=nil||len(data)>1<<20{return nil,errors.New("configured file exceeds size limit")}
    return data,nil
}

func run()error{
    if len(os.Args)!=3||os.Args[1]!="--state-dir"{
        return errors.New("usage: homelab-security-preflight --state-dir ABSOLUTE_COPY_DIRECTORY")
    }
    dir:=os.Args[2]
    if !filepath.IsAbs(dir)||filepath.Clean(dir)!=dir||
        dir=="/var/lib/homelab-securityd"{
        return errors.New("Go compatibility inspection requires an offline state copy")
    }
    info,err:=os.Lstat(dir)
    if err!=nil||!info.IsDir()||info.Mode().Perm()&0077!=0{
        return errors.New("Go compatibility copy must be an owner-only directory")
    }
    replay,err:=os.Lstat(filepath.Join(dir,"authenticator.json"))
    if err!=nil||!replay.Mode().IsRegular()||replay.Mode().Perm()&0077!=0{
        return errors.New("Go compatibility requires preserved replay state")
    }
    verifierBytes,err:=read("HOMELAB_SECURITY_PASSWORD_FILE",true)
    if err!=nil{return err}
    totpBytes,err:=read("HOMELAB_SECURITY_TOTP_FILE",true)
    if err!=nil{return err}
    policyBytes,err:=read("HOMELAB_SECURITY_POLICY_FILE",false)
    if err!=nil{return err}
    var verifier security.PassphraseRecord
    if err:=json.Unmarshal(verifierBytes,&verifier);err!=nil{
        return errors.New("invalid Go-compatible password verifier")
    }
    var policies []security.OperationPolicy
    if err:=json.Unmarshal(policyBytes,&policies);err!=nil{
        return errors.New("invalid Go-compatible operation policy")
    }
    authority,err:=security.NewAdminSessions(security.SessionConfig{
        Owner:os.Getenv("HOMELAB_SECURITY_OWNER"),
        Client:"platform-gateway",Verifier:verifier,
        TOTPSecret:strings.TrimSpace(string(totpBytes)),
        StateFile:filepath.Join(dir,"authenticator.json"),
        Policies:policies,
    })
    for i:=range totpBytes{totpBytes[i]=0}
    if err!=nil{return errors.New("Go cannot parse copied authentication state")}
    if os.Getenv("HOMELAB_SECURITY_VAULT_KEY_FILE")!=""{
        key,err:=read("HOMELAB_SECURITY_VAULT_KEY_FILE",true)
        if err!=nil{return err}
        if len(key)!=32{
            for i:=range key{key[i]=0}
            return errors.New("invalid Go-compatible vault key")
        }
        if _,err:=os.Lstat(filepath.Join(dir,"credentials.json"));err!=nil{
            for i:=range key{key[i]=0}
            return errors.New("copied credential state missing")
        }
        vault,err:=security.OpenVault(filepath.Join(dir,"credentials.json"),key)
        for i:=range key{key[i]=0}
        if err!=nil{return errors.New("Go cannot parse encrypted credential state")}
        for _,credential:=range vault.List(){
            err:=vault.WithCredential(credential.ID,credential.Provider,func(raw []byte)error{
                if len(raw)==0{return errors.New("empty decrypted credential")}
                return nil
            })
            if err!=nil{return errors.New("Go cannot authenticate encrypted credential")}
        }
        if err:=authority.SetVault(vault);err!=nil{return err}
    }else if _,err:=os.Lstat(filepath.Join(dir,"credentials.json"));err==nil{
        return errors.New("credential state cannot be inspected without a vault key")
    }else if !errors.Is(err,os.ErrNotExist){
        return errors.New("cannot inspect copied credential state")
    }
    if raw:=os.Getenv("HOMELAB_SECURITY_SERVICE_CREDENTIAL_GRANTS");raw!=""{
        if len(raw)>128<<10{return errors.New("Go credential policy is oversized")}
        var grants []security.ServiceCredentialGrant
        if err:=json.Unmarshal([]byte(raw),&grants);err!=nil||
            authority.SetServiceCredentialGrants(grants)!=nil{
            return errors.New("Go credential policy is incompatible")
        }
    }
    if raw:=os.Getenv("HOMELAB_SECURITY_OAUTH_PROVIDERS");raw!=""{
        var providers []security.Provider
        if err:=json.Unmarshal([]byte(raw),&providers);err!=nil{
            return errors.New("invalid Go provider configuration")
        }
        broker,err:=security.NewOAuthBroker(providers)
        if err!=nil{return errors.New("Go OAuth provider policy incompatible")}
        if err:=authority.SetOAuthBroker(broker,nil);err!=nil{return err}
    }
    if audience:=os.Getenv("HOMELAB_SECURITY_ACCESS_GATEWAY_AUD");audience!=""{
        jwks,err:=read("HOMELAB_SECURITY_ACCESS_JWKS_FILE",false)
        if err!=nil{return err}
        owner,err:=read("HOMELAB_SECURITY_ACCESS_OWNER_FILE",true)
        if err!=nil{return err}
        verifier,err:=security.NewAccessVerifier(
            os.Getenv("HOMELAB_SECURITY_ACCESS_ISSUER"),strings.TrimSpace(string(owner)),jwks)
        for i:=range owner{owner[i]=0}
        if err!=nil{return errors.New("Go pinned issuer configuration incompatible")}
        if err:=authority.SetAccessVerifier(verifier,audience);err!=nil{return err}
    }
    fmt.Println("Go authority accepts the independent copied state and protected policy.")
    return nil
}

func main(){
    if err:=run();err!=nil{
        fmt.Fprintln(os.Stderr,"Go security preflight denied:",err)
        os.Exit(1)
    }
}
