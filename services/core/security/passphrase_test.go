package security

import (
 "encoding/base64"
 "strings"
 "testing"
 "time"
)

func TestPassphraseDerivationAndOwnerUnlock(t *testing.T){
 pass:=[]byte("research-owner-passphrase")
 record,err:=HashPassphrase(pass);if err!=nil{t.Fatal(err)}
 if !record.Verify(pass)||record.Verify([]byte("incorrect-passphrase")){t.Fatal("passphrase verification failed")}
 unlock,err:=NewOwnerUnlock(record);if err!=nil{t.Fatal(err)}
 for i:=0;i<5;i++{
  if err:=unlock.Verify([]byte("incorrect-passphrase"));err==nil{t.Fatal("invalid passphrase accepted")}
 }
 if err:=unlock.Verify(pass);err==nil{t.Fatal("lockout bypassed")}
 unlock.now=func()time.Time{return time.Now().Add(6*time.Minute)}
 if err:=unlock.Verify(pass);err!=nil{t.Fatal(err)}
 if _,err:=HashPassphrase([]byte("short"));err==nil{t.Fatal("short passphrase accepted")}
 record.Iterations=1
 if record.Verify(pass){t.Fatal("weak iteration count accepted")}
 if _,err:=NewOwnerUnlock(record);err==nil{t.Fatal("weak verifier accepted")}
}

func TestPassphrasePBKDF2Vector(t *testing.T){
 actual:=derivePassphrase([]byte("password"),[]byte("salt"),2)
 expected,err:=base64.RawStdEncoding.DecodeString("rk0Mla9rRtMtCt/5KPBt0CowP47zwlHf1uLYWpVHTEM")
 if err!=nil{t.Fatal(err)}
 if len(expected)!=len(actual)||!strings.EqualFold(base64.RawStdEncoding.EncodeToString(actual[:]),base64.RawStdEncoding.EncodeToString(expected)){t.Fatalf("PBKDF2 vector mismatch: %x",actual)}
}
