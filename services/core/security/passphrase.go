package security

import (
 "crypto/hmac"
 "crypto/rand"
 "crypto/sha256"
 "crypto/subtle"
 "encoding/base64"
 "errors"
 "sync"
 "time"
)

const (
 passphraseIterations = 310_000
 minimumPassphraseBytes = 16
)

// PassphraseRecord is safe to store separately from the local owner secret.
// Verification is permitted only by the independently protected owner service.
type PassphraseRecord struct {
 Version int `json:"version"`
 Iterations int `json:"iterations"`
 Salt string `json:"salt"`
 Digest string `json:"digest"`
}

func HashPassphrase(passphrase []byte)(PassphraseRecord,error){
 if len(passphrase)<minimumPassphraseBytes || len(passphrase)>1024 {
  return PassphraseRecord{},errors.New("owner passphrase must contain 16-1024 bytes")
 }
 salt:=make([]byte,24)
 if _,err:=rand.Read(salt);err!=nil{return PassphraseRecord{},err}
 digest:=derivePassphrase(passphrase,salt,passphraseIterations)
 return PassphraseRecord{Version:1,Iterations:passphraseIterations,
 Salt:base64.RawStdEncoding.EncodeToString(salt),
 Digest:base64.RawStdEncoding.EncodeToString(digest[:])},nil
}

// Derive one 256-bit PBKDF2-HMAC-SHA256 block (RFC 8018, section 5.2).
func derivePassphrase(passphrase,salt []byte,iterations int)[32]byte{
 mac:=hmac.New(sha256.New,passphrase)
 _,_=mac.Write(salt)
 _,_=mac.Write([]byte{0,0,0,1})
 var output [32]byte
 u:=mac.Sum(nil)
 copy(output[:],u)
 for i:=1;i<iterations;i++{
  mac.Reset()
  _,_=mac.Write(u)
  u=mac.Sum(u[:0])
  for j:=range output{output[j]^=u[j]}
 }
 return output
}

func(r PassphraseRecord)Verify(passphrase []byte)bool{
 if r.Version!=1||r.Iterations<passphraseIterations||r.Iterations>2_000_000||len(passphrase)>1024{return false}
 salt,err1:=base64.RawStdEncoding.DecodeString(r.Salt)
 expected,err2:=base64.RawStdEncoding.DecodeString(r.Digest)
 if err1!=nil||err2!=nil||len(salt)!=24||len(expected)!=32{return false}
 actual:=derivePassphrase(passphrase,salt,r.Iterations)
 return subtle.ConstantTimeCompare(actual[:],expected)==1
}

// OwnerUnlock limits consecutive password guesses against a local operator socket.
type OwnerUnlock struct {
 mu sync.Mutex
 verifier PassphraseRecord
 failures int
 lockedUntil time.Time
 now func()time.Time
}

func NewOwnerUnlock(record PassphraseRecord)(*OwnerUnlock,error){
 if record.Version!=1||record.Iterations<passphraseIterations||record.Iterations>2_000_000{
  return nil,errors.New("invalid owner verifier")
 }
 if _,err:=base64.RawStdEncoding.DecodeString(record.Digest);err!=nil{return nil,err}
 if _,err:=base64.RawStdEncoding.DecodeString(record.Salt);err!=nil{return nil,err}
 return &OwnerUnlock{verifier:record,now:time.Now},nil
}

func(u *OwnerUnlock)Verify(passphrase []byte)error{
 u.mu.Lock()
 defer u.mu.Unlock()
 if u.now().Before(u.lockedUntil){return errors.New("owner unlock temporarily unavailable")}
 if !u.verifier.Verify(passphrase){
  u.failures++
  if u.failures>=5{
   u.lockedUntil=u.now().Add(5*time.Minute)
   u.failures=0
  }
  return errors.New("owner unlock denied")
 }
 u.failures=0
 return nil
}
