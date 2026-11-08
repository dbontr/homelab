package security

import (
 "crypto/hmac"
 "crypto/rand"
 "crypto/sha256"
 "encoding/base64"
 "encoding/hex"
 "encoding/json"
 "io"

 "github.com/dbontr/homelab-new/system/canonicaljson"
 "errors"
 "fmt"
 "os"
 "path/filepath"
 "strings"
 "sync"
 "time"
)

const MaxGrantDuration = 2 * time.Minute

type claim struct {
 Version int `json:"v"`
 ID string `json:"id"`
 Action string `json:"action"`
 Target string `json:"target"`
 Digest string `json:"digest"`
 Expires int64 `json:"expires"`
}

// Authority is held by an independently privileged executor, never the MCP gateway.
type Authority struct {
 mu sync.Mutex
 key []byte
 path string
 used map[string]int64
 now func() time.Time
}

func NewAuthority(key []byte, replayFile string) (*Authority,error) {
 if len(key)<32 || replayFile=="" {return nil,errors.New("executor requires a secret key and durable replay file")}
 a:=&Authority{key:append([]byte(nil),key...),path:replayFile,used:map[string]int64{},now:time.Now}
 info,err:=os.Lstat(replayFile)
 if err==nil && (!info.Mode().IsRegular()||info.Mode().Perm()&0077!=0){
   return nil,errors.New("executor replay file must be an owner-only regular file")
 }
 if err!=nil&&!errors.Is(err,os.ErrNotExist){return nil,err}
 f,err:=os.Open(replayFile)
 if errors.Is(err,os.ErrNotExist) {return a,nil}
 if err!=nil{return nil,err}
 defer f.Close()
 opened,err:=f.Stat()
 if err!=nil{return nil,err}
 if !opened.Mode().IsRegular()||opened.Mode().Perm()&0077!=0{
  return nil,errors.New("executor replay file must be an owner-only regular file")
 }
 b,err:=io.ReadAll(io.LimitReader(f,(1<<20)+1))
 if err!=nil{return nil,err}
 if len(b)>1<<20{return nil,errors.New("executor replay state exceeds size limit")}
 if err:=json.Unmarshal(b,&a.used);err!=nil||a.used==nil{return nil,errors.New("invalid executor replay state")}
 for id,expiry:=range a.used {
  if len(id)!=48||expiry<=0{return nil,errors.New("invalid executor replay entry")}
  decoded,err:=hex.DecodeString(id)
  if err!=nil||len(decoded)!=24{return nil,errors.New("invalid executor replay entry")}
 }
 return a,nil
}

func digestArgs(args json.RawMessage)(string,error){
    encoded,_,err:=canonicaljson.Object(args)
    if err!=nil{return "",err}
    sum:=sha256.Sum256(encoded)
    return hex.EncodeToString(sum[:]),nil
}

func(a *Authority)mac(payload string)[]byte{
 m:=hmac.New(sha256.New,a.key)
 _,_=m.Write([]byte(payload))
 return m.Sum(nil)
}

// Issue is available only through the restricted local owner approval channel.
func(a *Authority)Issue(action,target string,args json.RawMessage,ttl time.Duration)(string,error){
 if action==""||target==""||len(action)>128||len(target)>128||ttl<=0||ttl>MaxGrantDuration{
   return "",errors.New("invalid approval scope or duration")
 }
 digest,err:=digestArgs(args);if err!=nil{return "",err}
 nonce:=make([]byte,24)
 if _,err:=rand.Read(nonce);err!=nil{return "",err}
 c:=claim{Version:1,ID:hex.EncodeToString(nonce),Action:action,Target:target,Digest:digest,Expires:a.now().Add(ttl).Unix()}
 b,err:=json.Marshal(c);if err!=nil{return "",err}
 payload:=base64.RawURLEncoding.EncodeToString(b)
 return payload+"."+base64.RawURLEncoding.EncodeToString(a.mac(payload)),nil
}

// Consume checks scope and persists replay protection before execution.
func(a *Authority)Consume(grant,action,target string,args json.RawMessage)error{
 parts:=strings.Split(grant,".")
 if len(parts)!=2||len(grant)>4096{return errors.New("invalid executor approval")}
 signature,err:=base64.RawURLEncoding.DecodeString(parts[1])
 if err!=nil||!hmac.Equal(signature,a.mac(parts[0])){return errors.New("invalid executor approval")}
 payload,err:=base64.RawURLEncoding.DecodeString(parts[0])
 if err!=nil{return errors.New("invalid executor approval")}
 var c claim
 if err:=json.Unmarshal(payload,&c);err!=nil||c.Version!=1||len(c.ID)!=48{
   return errors.New("invalid executor approval")
 }
 digest,err:=digestArgs(args);if err!=nil{return err}
 now:=a.now().Unix()
 if c.Expires<=now||c.Expires>now+int64(MaxGrantDuration.Seconds())||c.Action!=action||c.Target!=target||c.Digest!=digest{
   return errors.New("executor approval scope mismatch or expired")
 }
 a.mu.Lock();defer a.mu.Unlock()
 if _,ok:=a.used[c.ID];ok{return errors.New("executor approval already consumed")}
 next:=make(map[string]int64,len(a.used)+1)
 for id,expiry:=range a.used {if expiry>now {next[id]=expiry}}
 next[c.ID]=c.Expires
 if err:=a.saveUsed(next);err!=nil{return fmt.Errorf("persist executor approval: %w",err)}
 a.used=next
 return nil
}

func(a *Authority)saveUsed(next map[string]int64)error{
 if err:=os.MkdirAll(filepath.Dir(a.path),0700);err!=nil{return err}
 b,err:=json.Marshal(next);if err!=nil{return err}
 if len(b)>1<<20{return errors.New("executor replay state exceeds size limit")}
 f,err:=os.CreateTemp(filepath.Dir(a.path),".grants-*");if err!=nil{return err}
 defer os.Remove(f.Name())
 if err:=f.Chmod(0600);err!=nil{f.Close();return err}
 if _,err:=f.Write(b);err!=nil{f.Close();return err}
 if err:=f.Sync();err!=nil{f.Close();return err}
 if err:=f.Close();err!=nil{return err}
 if err:=os.Rename(f.Name(),a.path);err!=nil{return err}
 if dir,err:=os.Open(filepath.Dir(a.path));err==nil{_=dir.Sync();_=dir.Close()}
 return nil
}
