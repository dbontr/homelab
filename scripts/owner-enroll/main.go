// Command owner-enroll performs one-time, locally persisted administrator enrollment.
package main

import (
    "crypto/hmac"
    "math/big"
    "crypto/rsa"
    "crypto"
    "crypto/rand"
    "crypto/sha256"
    "crypto/subtle"
    "encoding/base32"
    "encoding/base64"
    "encoding/json"
    "fmt"
    "io"
    "log"
    "net/http"
    "os"
    "path/filepath"
    "strings"
    "sync"
    "time"
)
const destination="/srv/homelab/secrets/security"
const publicOrigin="https://enroll.dbontr.me"
type app struct {mu sync.Mutex; csrf string; done bool; failures int; deadline time.Time; server *http.Server}
func hashPassword(password string,salt []byte)[]byte{
    m:=hmac.New(sha256.New,[]byte(password))
    m.Write(salt);m.Write([]byte{0,0,0,1})
    previous:=m.Sum(nil)
    sum:=append([]byte(nil),previous...)
    for i:=1;i<310000;i++{
        m.Reset();m.Write(previous);previous=m.Sum(previous[:0])
        for k:=range sum{sum[k]^=previous[k]}
    }
    return sum
}
func checkTarget()error{
    if os.Geteuid()!=0{return fmt.Errorf("root required")}
    s,err:=os.Lstat(destination)
    if err!=nil||!s.IsDir()||s.Mode().Perm()!=0700||s.Mode()&os.ModeSymlink!=0{return fmt.Errorf("private directory required")}
    for _,file:=range []string{"owner-verifier.json","owner-totp"}{
        _,err=os.Lstat(filepath.Join(destination,file))
        if err==nil{return fmt.Errorf("credentials already exist")}
        if !os.IsNotExist(err){return fmt.Errorf("credential file inspection failed")}
    }
    return nil
}
func persist(password,totp string)error{
    if err:=checkTarget();err!=nil{return err}
    salt:=make([]byte,24)
    if _,err:=rand.Read(salt);err!=nil{return err}
    b:=base64.RawStdEncoding
    verifier:=map[string]any{"version":1,"iterations":310000,"salt":b.EncodeToString(salt),"digest":b.EncodeToString(hashPassword(password,salt))}
    value,err:=json.Marshal(verifier);if err!=nil{return err}
    entries:=[]struct{name string;data []byte}{{"owner-verifier.json",append(value,'\n')},{"owner-totp",[]byte(totp+"\n")}}
    created:=[]string{}
    defer func(){for _,p:=range created{_ =os.Remove(p)}}()
    for _,v:=range entries{
        name:=filepath.Join(destination,v.name)
        f,err:=os.OpenFile(name,os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600);if err!=nil{return err}
        created=append(created,name)
        _,err=f.Write(v.data);if err==nil{err=f.Sync()}
        cerr:=f.Close();if err!=nil{return err};if cerr!=nil{return cerr}
    }
    dir,err:=os.Open(destination);if err!=nil{return err};defer dir.Close()
    if err:=dir.Sync();err!=nil{return err}
    created=nil
    return nil
}
const audience="f7ff144af280e780e5eabee814d5f6809d83b3a3d1acd1c7666bafd583d0f308"
func verifiedJWT(token string) bool {
    if len(token)<100 || len(token)>8192 {return false}
    parts:=strings.Split(token,".")
    if len(parts)!=3{return false}
    decode:=base64.RawURLEncoding.DecodeString
    headerData,err:=decode(parts[0]);if err!=nil{return false}
    payloadData,err:=decode(parts[1]);if err!=nil{return false}
    signature,err:=decode(parts[2]);if err!=nil{return false}
    var header struct{ Alg string `json:"alg"`; Kid string `json:"kid"` }
    var claims struct{
        Iss string `json:"iss"`
        Email string `json:"email"`
        Aud []string `json:"aud"`
        Exp int64 `json:"exp"`
        Nbf int64 `json:"nbf"`
        Iat int64 `json:"iat"`
    }
    if json.Unmarshal(headerData,&header)!=nil || json.Unmarshal(payloadData,&claims)!=nil {return false}
    now:=time.Now().Unix()
    if header.Alg!="RS256" || header.Kid=="" || claims.Iss!="https://dbontr.cloudflareaccess.com" ||
       !strings.EqualFold(claims.Email,"dbontr33@hotmail.com") || claims.Exp<=now ||
       claims.Iat>now+30 || claims.Nbf>now+30 {return false}
    match:=false
    for _,a:=range claims.Aud {if a==audience{match=true}}
    if !match{return false}
    pem,err:=os.ReadFile("/srv/homelab/secrets/security/access-jwks.json")
    if err!=nil || len(pem)>65536{return false}
    var jwks struct{ Keys []struct{
        Kid string `json:"kid"`
        Kty string `json:"kty"`
        Alg string `json:"alg"`
        N string `json:"n"`
        E string `json:"e"`
    } `json:"keys"` }
    if json.Unmarshal(pem,&jwks)!=nil {return false}
    for _,key:=range jwks.Keys {
        if key.Kid!=header.Kid || key.Kty!="RSA" {continue}
        modulus,err:=decode(key.N);if err!=nil{return false}
        exponent,err:=decode(key.E);if err!=nil||len(exponent)<1||len(exponent)>4{return false}
        e:=0
        for _,v:=range exponent {e=(e<<8)|int(v)}
        if len(modulus)<256||e<3{return false}
        digest:=sha256.Sum256([]byte(parts[0]+"."+parts[1]))
        return rsa.VerifyPKCS1v15(&rsa.PublicKey{N:new(big.Int).SetBytes(modulus),E:e},crypto.SHA256,digest[:],signature)==nil
    }
    return false
}

func (a *app) ServeHTTP(w http.ResponseWriter,r *http.Request){
    for k,v:=range map[string]string{
        "Cache-Control":"private, no-store, max-age=0",
        "Pragma":"no-cache","X-Content-Type-Options":"nosniff",
        "Referrer-Policy":"no-referrer","X-Frame-Options":"DENY",
        "Content-Security-Policy":"default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'",
        "Content-Type":"text/html; charset=utf-8",
    }{w.Header().Set(k,v)}
    if r.Host!="enroll.dbontr.me"||!verifiedJWT(r.Header.Get("Cf-Access-Jwt-Assertion")){http.Error(w,"Access authentication required",403);return}
    a.mu.Lock();defer a.mu.Unlock()
    if time.Now().After(a.deadline){http.Error(w,"Enrollment expired",410);return}
    if a.done{fmt.Fprint(w,"<h2>Enrollment already completed.</h2>");return}
    if a.failures>=5{http.Error(w,"Enrollment locked",429);return}
    if r.URL.Path!="/"{http.NotFound(w,r);return}
    if r.Method==http.MethodGet{
        fmt.Fprintf(w,"<!doctype html><html lang='en'><head><meta charset='utf-8'><meta name='robots' content='noindex,nofollow,noarchive'><meta name='viewport' content='width=device-width,initial-scale=1'><title>Homelab owner enrollment</title><style>body{font:16px system-ui;background:#111827;color:#fff;max-width:560px;margin:6vh auto;padding:24px}label{display:block;margin:22px 0 6px}input,button{box-sizing:border-box;width:100%%;padding:12px;font:inherit}button{margin-top:24px}</style></head><body><h1>Enroll administrator on Saturn</h1><p>Enter the password and TOTP Base32 seed you recorded. They are sent directly to Saturn through Cloudflare Access and are never stored in Cloudflare KV.</p><form method='POST'><input type='hidden' name='csrf' value='%s'><label>Password</label><input name='password' type='password' required minlength='16' maxlength='1024' autocomplete='new-password'><label>Confirm password</label><input name='confirm' type='password' required minlength='16' maxlength='1024' autocomplete='off'><label>TOTP Base32 secret</label><input name='totp' type='password' required minlength='32' maxlength='208' autocomplete='off'><button type='submit'>Install credentials on Saturn</button></form><p>Temporary, owner-only endpoint. Do not enter your root login password.</p></body></html>",a.csrf)
        return
    }
    if r.Method!=http.MethodPost{http.Error(w,"Method not allowed",405);return}
    if r.Header.Get("Origin")!=publicOrigin||!strings.HasPrefix(r.Header.Get("Content-Type"),"application/x-www-form-urlencoded"){a.failures++;http.Error(w,"Invalid origin",403);return}
    r.Body=http.MaxBytesReader(w,r.Body,4096)
    if r.ParseForm()!=nil{a.failures++;http.Error(w,"Invalid form",400);return}
    if subtle.ConstantTimeCompare([]byte(r.PostForm.Get("csrf")),[]byte(a.csrf))!=1{a.failures++;http.Error(w,"Invalid session",403);return}
    password:=r.PostForm.Get("password")
    totp:=strings.ToUpper(strings.TrimSpace(r.PostForm.Get("totp")))
    secret,err:=base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(totp)
    if err!=nil||len(secret)<20||len(secret)>128||len(totp)<32||len(totp)>208||len(password)<16||len(password)>1024||password!=r.PostForm.Get("confirm"){
        a.failures++;http.Error(w,"Invalid password or authenticator secret",400);return
    }
    if err:=persist(password,totp);err!=nil{log.Printf("enrollment failed: %T",err);http.Error(w,"Installation failed; inspect Saturn",503);return}
    a.done=true
    fmt.Fprint(w,"<h2>Credentials installed on Saturn.</h2><p>Enrollment is complete; this endpoint is closing.</p>")
    go func(){time.Sleep(3*time.Second);_ =a.server.Close()}()
}
func main(){
    if err:=checkTarget();err!=nil{log.Fatal(err)}
    random:=make([]byte,24);if _,err:=io.ReadFull(rand.Reader,random);err!=nil{log.Fatal(err)}
    a:=&app{csrf:base64.RawURLEncoding.EncodeToString(random),deadline:time.Now().Add(30*time.Minute)}
    a.server=&http.Server{Addr:"127.0.0.1:8127",Handler:a,ReadHeaderTimeout:5*time.Second,ReadTimeout:10*time.Second,WriteTimeout:10*time.Second,MaxHeaderBytes:4096}
    time.AfterFunc(time.Until(a.deadline),func(){_ =a.server.Close()})
    log.Print("owner enrollment receiver ready on 127.0.0.1:8127")
    if err:=a.server.ListenAndServe();err!=nil&&err!=http.ErrServerClosed{log.Fatal(err)}
}
