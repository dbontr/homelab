package security

import (
 "bytes"
 "crypto/rand"
 "crypto/rsa"
 "crypto/x509"
 "crypto/x509/pkix"
 "encoding/pem"
 "math/big"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

func certificateMaterialFixture(t *testing.T)(string,string){
 t.Helper()
 dir:=t.TempDir()
 key,err:=rsa.GenerateKey(rand.Reader,2048)
 if err!=nil{t.Fatal(err)}
 now:=time.Now()
 template:=&x509.Certificate{
  SerialNumber:big.NewInt(1),
  Subject:pkix.Name{CommonName:"trusted"},
  NotBefore:now.Add(-time.Hour),NotAfter:now.Add(time.Hour),
  KeyUsage:x509.KeyUsageDigitalSignature,
  ExtKeyUsage:[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}},
 der,err:=x509.CreateCertificate(rand.Reader,template,template,&key.PublicKey,key)
 if err!=nil{t.Fatal(err)}
 certPath,keyPath:=filepath.Join(dir,"cert.pem"),filepath.Join(dir,"key.pem")
 if err:=os.WriteFile(certPath,pem.EncodeToMemory(&pem.Block{Type:"CERTIFICATE",Bytes:der}),0444);err!=nil{t.Fatal(err)}
 if err:=os.Chmod(certPath,0444);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(keyPath,pem.EncodeToMemory(&pem.Block{Type:"RSA PRIVATE KEY",Bytes:x509.MarshalPKCS1PrivateKey(key)}),0600);err!=nil{t.Fatal(err)}
 return certPath,keyPath
}

func TestTLSPrivateKeyRequiresOwnerOnlyAccess(t *testing.T){
 cert,key:=certificateMaterialFixture(t)
 if _,err:=LoadTLSIdentity(cert,key);err!=nil{t.Fatal(err)}
 if _,err:=LoadTLSRoots(cert);err!=nil{t.Fatal(err)}
 for _,mode:=range []os.FileMode{0644,0640,0660}{
  if err:=os.Chmod(key,mode);err!=nil{t.Fatal(err)}
  if _,err:=LoadTLSIdentity(cert,key);err==nil{t.Fatalf("unsafe private key mode %04o accepted",mode)}
 }
 if err:=os.Chmod(key,0600);err!=nil{t.Fatal(err)}
 link:=filepath.Join(filepath.Dir(key),"linked-key")
 if err:=os.Symlink(key,link);err!=nil{t.Fatal(err)}
 if _,err:=LoadTLSIdentity(cert,link);err==nil{t.Fatal("key symlink accepted")}
 if err:=os.Remove(link);err!=nil{t.Fatal(err)}
 if err:=os.Symlink(cert,link);err!=nil{t.Fatal(err)}
 if _,err:=LoadTLSIdentity(link,key);err==nil{t.Fatal("certificate symlink accepted")}
}

func TestTLSMaterialRequiresBoundedValidFiles(t *testing.T){
 cert,key:=certificateMaterialFixture(t)
 if err:=os.WriteFile(key,bytes.Repeat([]byte("X"),maxTLSFileBytes+1),0600);err!=nil{t.Fatal(err)}
 if _,err:=LoadTLSIdentity(cert,key);err==nil{t.Fatal("oversized TLS key accepted")}
 if _,err:=LoadTLSRoots(cert);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(cert,[]byte(strings.Repeat("invalid",128)),0444);err!=nil{t.Fatal(err)}
 if _,err:=LoadTLSRoots(cert);err==nil{t.Fatal("invalid trust roots accepted")}
 if _,err:=LoadTLSIdentity("relative-cert",key);err==nil{t.Fatal("relative path accepted")}
}
