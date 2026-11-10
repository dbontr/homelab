package security

import (
 "crypto/aes"
 "crypto/cipher"
 "crypto/rand"
 "crypto/sha256"
 "encoding/hex"
 "encoding/base64"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "syscall"
 "os"
 "path/filepath"
 "sort"
 "strings"
 "sync"
 "time"
)

type CredentialHandle struct {
 ID string `json:"id"`
 Provider string `json:"provider"`
 CreatedAt time.Time `json:"created_at"`
}

type storedCredential struct {
 CredentialHandle
 Nonce string `json:"nonce"`
 Data string `json:"data"`
}

// Vault is owned by a trusted local credential process, not the MCP gateway.
type Vault struct {
 mu sync.RWMutex
 key [32]byte
 path string
 records map[string]storedCredential
}

func OpenVault(path string, secret []byte)(*Vault,error){
 if len(secret)!=32||path==""||!filepath.IsAbs(path){
   return nil,errors.New("credential vault requires an absolute path and 256-bit key")
 }
 v:=&Vault{path:path,records:map[string]storedCredential{}}
 copy(v.key[:],secret)
 file,err:=os.OpenFile(path,os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC,0)
 if errors.Is(err,os.ErrNotExist){return v,nil}
 if err!=nil{return nil,errors.New("credential vault cannot be opened")}
 defer file.Close()
 info,err:=file.Stat()
 if err!=nil||!info.Mode().IsRegular()||info.Mode().Perm()&0077!=0{
   return nil,errors.New("credential vault must be an owner-only regular file")
 }
 data,err:=io.ReadAll(io.LimitReader(file,(8<<20)+1))
 if err!=nil{return nil,err}
 if len(data)>8<<20{return nil,errors.New("credential vault exceeds size limit")}
 if err:=json.Unmarshal(data,&v.records);err!=nil||v.records==nil{
   return nil,errors.New("invalid credential vault")
 }
 for id,rec:=range v.records{
   if id!=rec.ID||rec.Provider==""||rec.Nonce==""||rec.Data==""{
     return nil,errors.New("invalid credential record")
   }
 }
 return v,nil
}

func(v *Vault)seal(provider,id string,secret []byte)(storedCredential,error){
 block,err:=aes.NewCipher(v.key[:]);if err!=nil{return storedCredential{},err}
 gcm,err:=cipher.NewGCM(block);if err!=nil{return storedCredential{},err}
 nonce:=make([]byte,gcm.NonceSize())
 if _,err:=rand.Read(nonce);err!=nil{return storedCredential{},err}
 ciphertext:=gcm.Seal(nil,nonce,secret,[]byte(provider+":"+id))
 return storedCredential{
  CredentialHandle:CredentialHandle{ID:id,Provider:provider,CreatedAt:time.Now().UTC()},
  Nonce:base64.RawStdEncoding.EncodeToString(nonce),
  Data:base64.RawStdEncoding.EncodeToString(ciphertext),
 },nil
}

func(v *Vault)Store(provider string,secret []byte)(CredentialHandle,error){
 if provider==""||len(provider)>64||strings.ContainsAny(provider,"\n\r\t/:\\")||len(secret)==0||len(secret)>32768{
   return CredentialHandle{},errors.New("invalid credential metadata")
 }
 v.mu.Lock();defer v.mu.Unlock()
 if len(v.records)>=1024{return CredentialHandle{},errors.New("credential vault is full")}
 id,err:=randomURLToken(24);if err!=nil{return CredentialHandle{},err}
 rec,err:=v.seal(provider,id,secret);if err!=nil{return CredentialHandle{},err}
 next:=make(map[string]storedCredential,len(v.records)+1)
 for k,entry:=range v.records{next[k]=entry}
 next[id]=rec
 if err:=v.persist(next);err!=nil{return CredentialHandle{},err}
 v.records=next
 return rec.CredentialHandle,nil
}

// CredentialRevision is an internal fingerprint of one encrypted record.
// Rotation changes the revision without exposing the protected material.
func credentialRevision(rec storedCredential)string{
 sum:=sha256.Sum256([]byte(rec.Provider+":"+rec.ID+":"+rec.Nonce+":"+rec.Data))
 return hex.EncodeToString(sum[:])
}

func(v *Vault)Version(handle,provider string)(string,error){
 v.mu.Lock();defer v.mu.Unlock()
 rec,ok:=v.records[handle]
 if !ok||rec.Provider!=provider{return "",os.ErrNotExist}
 return credentialRevision(rec),nil
}

// Rotate replaces a secret atomically under the same opaque handle. Previously
// prepared scoped capabilities are invalidated by their stored revision.
func(v *Vault)Rotate(handle,provider string,secret []byte)(CredentialHandle,error){
 if len(secret)==0||len(secret)>32768{return CredentialHandle{},errors.New("invalid credential length")}
 v.mu.Lock();defer v.mu.Unlock()
 original,ok:=v.records[handle]
 if !ok||original.Provider!=provider{return CredentialHandle{},os.ErrNotExist}
 record,err:=v.seal(provider,handle,secret)
 if err!=nil{return CredentialHandle{},err}
 record.CreatedAt=original.CreatedAt
 next:=make(map[string]storedCredential,len(v.records))
 for id,item:=range v.records{next[id]=item}
 next[handle]=record
 if err:=v.persist(next);err!=nil{return CredentialHandle{},err}
 v.records=next
 return record.CredentialHandle,nil
}

func(v *Vault)WithCredential(handle,provider string,use func([]byte)error)error{
 return v.WithCredentialVersion(handle,provider,"",use)
}

// WithCredentialVersion refuses a capability issued against a previous
// encrypted record, including one that was rotated under the same handle.
// A read lock serializes credential use against owner rotation and deletion.
// Trusted callers must not mutate this vault inside the use callback.
func(v *Vault)WithCredentialVersion(handle,provider,version string,use func([]byte)error)error{
 if use==nil{return errors.New("credential operation is required")}
 v.mu.RLock()
 defer v.mu.RUnlock()
 rec,ok:=v.records[handle]
 if !ok||rec.Provider!=provider{return os.ErrNotExist}
 if version!=""&&credentialRevision(rec)!=version{return errors.New("credential capability invalidated by rotation")}
 block,err:=aes.NewCipher(v.key[:]);if err!=nil{return err}
 gcm,err:=cipher.NewGCM(block);if err!=nil{return err}
 nonce,err:=base64.RawStdEncoding.DecodeString(rec.Nonce)
 if err!=nil{return errors.New("invalid encrypted credential")}
 data,err:=base64.RawStdEncoding.DecodeString(rec.Data)
 if err!=nil{return errors.New("invalid encrypted credential")}
 plain,err:=gcm.Open(nil,nonce,data,[]byte(provider+":"+handle))
 if err!=nil{return errors.New("credential could not be decrypted")}
 defer func(){for i:=range plain{plain[i]=0}}()
 return use(plain)
}

// UpdateCredential applies a trusted transformation to a decrypted secret,
// then atomically replaces the ciphertext only if that exact record is still
// current. Network work occurs outside the vault mutex so revocation remains
// responsive; a concurrent rotation or deletion causes a fail-closed conflict.
func(v *Vault)UpdateCredential(handle,provider string,transform func([]byte)([]byte,error))error{
 if transform==nil{return errors.New("credential update requires a trusted transformation")}
 v.mu.Lock()
 current,ok:=v.records[handle]
 v.mu.Unlock()
 if !ok||current.Provider!=provider{return os.ErrNotExist}
 block,err:=aes.NewCipher(v.key[:]);if err!=nil{return err}
 gcm,err:=cipher.NewGCM(block);if err!=nil{return err}
 nonce,err:=base64.RawStdEncoding.DecodeString(current.Nonce)
 if err!=nil{return errors.New("invalid encrypted credential")}
 ciphertext,err:=base64.RawStdEncoding.DecodeString(current.Data)
 if err!=nil{return errors.New("invalid encrypted credential")}
 plaintext,err:=gcm.Open(nil,nonce,ciphertext,[]byte(provider+":"+handle))
 if err!=nil{return errors.New("credential could not be decrypted")}
 defer func(){for i:=range plaintext{plaintext[i]=0}}()
 nextSecret,err:=transform(plaintext)
 if err!=nil{return err}
 defer func(){for i:=range nextSecret{nextSecret[i]=0}}()
 if len(nextSecret)==0||len(nextSecret)>32768{return errors.New("invalid updated credential size")}
 v.mu.Lock();defer v.mu.Unlock()
 latest,exists:=v.records[handle]
 if !exists||latest.Provider!=provider||latest.Nonce!=current.Nonce||latest.Data!=current.Data{
  return errors.New("credential changed during protected update")
 }
 sealed,err:=v.seal(provider,handle,nextSecret)
 if err!=nil{return err}
 sealed.CreatedAt=current.CreatedAt
 next:=make(map[string]storedCredential,len(v.records))
 for id,entry:=range v.records{next[id]=entry}
 next[handle]=sealed
 if err:=v.persist(next);err!=nil{return err}
 v.records=next
 return nil
}

func(v *Vault)List()[]CredentialHandle{
 v.mu.Lock();defer v.mu.Unlock()
 out:=make([]CredentialHandle,0,len(v.records))
 for _,item:=range v.records{out=append(out,item.CredentialHandle)}
 sort.Slice(out,func(i,j int)bool{return out[i].ID<out[j].ID})
 return out
}

func(v *Vault)Delete(handle string)error{
 v.mu.Lock();defer v.mu.Unlock()
 if _,ok:=v.records[handle];!ok{return os.ErrNotExist}
 next:=make(map[string]storedCredential,len(v.records)-1)
 for id,item:=range v.records{if id!=handle{next[id]=item}}
 if err:=v.persist(next);err!=nil{return err}
 v.records=next
 return nil
}

func(v *Vault)persist(next map[string]storedCredential)error{
 if err:=os.MkdirAll(filepath.Dir(v.path),0700);err!=nil{return err}
 encoded,err:=json.Marshal(next);if err!=nil{return err}
 if len(encoded)>8<<20{return errors.New("credential vault exceeds size limit")}
 f,err:=os.CreateTemp(filepath.Dir(v.path),".credentials-*");if err!=nil{return err}
 defer os.Remove(f.Name())
 if err:=f.Chmod(0600);err!=nil{f.Close();return err}
 if _,err:=f.Write(encoded);err!=nil{f.Close();return err}
 if err:=f.Sync();err!=nil{f.Close();return err}
 if err:=f.Close();err!=nil{return err}
 if err:=os.Rename(f.Name(),v.path);err!=nil{return fmt.Errorf("persist credential vault: %w",err)}
 if dir,err:=os.Open(filepath.Dir(v.path));err==nil{_=dir.Sync();_=dir.Close()}
 return nil
}
