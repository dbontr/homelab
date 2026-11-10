package security

import (
 "bytes"
 "os"
 "path/filepath"
 "testing"

 "github.com/dbontr/homelab-new/system/canonicaljson"
)

// These vectors are consumed by the independent Rust security authority.
func TestSecurityAuthorityWireCompatibilityVectors(t *testing.T) {
 record:=PassphraseRecord{
  Version:1,Iterations:310000,
  Salt:"YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4",
  Digest:"l9m+PJ5k/gZlE6OL9Ek9YfJ7L5ajP+9F+d5Z1mp4TXg",
 }
 if !record.Verify([]byte("correct horse battery staple"))||
  record.Verify([]byte("wrong password")){
  t.Fatal("PBKDF2 owner enrollment vector changed")
 }
 if code:=TOTPCode([]byte("12345678901234567890"),59/30);code!="287082"{
  t.Fatal("RFC 6238 six-digit TOTP vector changed")
 }
 canonical,_,err:=canonicaljson.Object([]byte(`{"z":1,"a":{"y":true,"x":null}}`))
 if err!=nil||!bytes.Equal(canonical,[]byte(`{"a":{"x":null,"y":true},"z":1}`)){
  t.Fatalf("canonical operation arguments changed: %s %v",canonical,err)
 }
 fixture:=`{"abcdefghijklmnopqrstuvwxABCDEFGH":{"created_at":"2026-11-10T13:00:00Z","data":"DLSMWnGi41gg6TbuAat8MZmC8aPOp8dmn7s/5cVdSUxwDw","id":"abcdefghijklmnopqrstuvwxABCDEFGH","nonce":"AAECAwQFBgcICQoL","provider":"research"}}`
 path:=filepath.Join(privateDir(t),"credentials.json")
 if err:=os.WriteFile(path,[]byte(fixture),0600);err!=nil{t.Fatal(err)}
 vault,err:=OpenVault(path,bytes.Repeat([]byte{'k'},32))
 if err!=nil{t.Fatal(err)}
 err=vault.WithCredential("abcdefghijklmnopqrstuvwxABCDEFGH","research",func(secret []byte)error{
  if !bytes.Equal(secret,[]byte("fixture-credential")){t.Error("Go vault decryption differs from Rust contract")}
  return nil
 })
 if err!=nil{t.Fatalf("vault ciphertext compatibility failed: %v",err)}
}
