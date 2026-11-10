package security

import (
 "crypto/tls"
 "crypto/x509"
 "errors"
 "io"
 "os"
 "path/filepath"
 "syscall"
)

const maxTLSFileBytes=1<<20

func readTLSMaterial(path string,private bool)([]byte,error){
 if !filepath.IsAbs(path)||filepath.Clean(path)!=path{
  return nil,errors.New("TLS material requires a canonical absolute path")
 }
 file,err:=os.OpenFile(path,os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC,0)
 if err!=nil{return nil,errors.New("TLS material cannot be opened")}
 defer file.Close()
 info,err:=file.Stat()
 if err!=nil||!info.Mode().IsRegular(){return nil,errors.New("TLS material must be a regular file")}
 perms:=info.Mode().Perm()
 if (private&&perms&0077!=0)||(!private&&perms&0022!=0){
  return nil,errors.New("TLS material has unsafe access permissions")
 }
 data,err:=io.ReadAll(io.LimitReader(file,maxTLSFileBytes+1))
 if err!=nil||len(data)>maxTLSFileBytes{return nil,errors.New("TLS material exceeds size limit")}
 return data,nil
}

// LoadTLSIdentity admits a public certificate and an owner-only private key
// through no-follow, size-bounded file descriptors.
func LoadTLSIdentity(certificatePath,keyPath string)(tls.Certificate,error){
 certificate,err:=readTLSMaterial(certificatePath,false)
 if err!=nil{return tls.Certificate{},err}
 key,err:=readTLSMaterial(keyPath,true)
 if err!=nil{return tls.Certificate{},err}
 defer clear(key)
 identity,err:=tls.X509KeyPair(certificate,key)
 if err!=nil{return tls.Certificate{},errors.New("invalid TLS certificate or private key")}
 return identity,nil
}

// LoadTLSRoots admits bounded read-only certificate authorities without
// conflating public trust roots with owner-only private keys.
func LoadTLSRoots(path string)(*x509.CertPool,error){
 pem,err:=readTLSMaterial(path,false)
 if err!=nil{return nil,err}
 roots:=x509.NewCertPool()
 if !roots.AppendCertsFromPEM(pem){return nil,errors.New("invalid TLS trust roots")}
 return roots,nil
}
