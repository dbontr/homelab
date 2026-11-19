package main

import (
 "context"
 "crypto/rsa"
 "crypto/x509"
 "encoding/pem"
 "errors"
 "io"
 "log"
 "net"
 "net/http"
 "os"
 "os/signal"
 "syscall"
 "time"

 "github.com/dbontr/homelab-new/system/machineidentity"
 "github.com/dbontr/homelab-new/system/security"
)

func credential(path string,limit int64)([]byte,error){
 if path==""{return nil,errors.New("protected credential path required")}
 f,err:=os.OpenFile(path,os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC,0)
 if err!=nil{return nil,errors.New("protected credential unavailable")}
 defer f.Close()
 stat,err:=f.Stat()
 if err!=nil||!stat.Mode().IsRegular()||stat.Size()<32||stat.Size()>limit{
  return nil,errors.New("unsafe credential file")
 }
 inode,ok:=stat.Sys().(*syscall.Stat_t)
 if !ok||inode.Nlink!=1{return nil,errors.New("credential file must have a single link")}
 if !security.PrivateCredentialFileAllowed(path,stat.Mode(),inode.Uid){
  return nil,errors.New("unsafe credential file")
 }
 return io.ReadAll(io.LimitReader(f,limit+1))
}

func main(){
 log.SetFlags(log.LstdFlags|log.LUTC)
 privatePEM,err:=credential(os.Getenv("HOMELAB_IDENTITY_KEY_FILE"),16384)
 if err!=nil{log.Fatal(err)}
 block,extra:=pem.Decode(privatePEM)
 if block==nil||len(extra)!=0{log.Fatal("invalid signing key encoding")}
 var signingKey *rsa.PrivateKey
 if block.Type=="RSA PRIVATE KEY"{
  signingKey,err=x509.ParsePKCS1PrivateKey(block.Bytes)
 }else if block.Type=="PRIVATE KEY"{
  parsed,parseErr:=x509.ParsePKCS8PrivateKey(block.Bytes)
  if parseErr!=nil{err=parseErr}else{
   var ok bool
   signingKey,ok=parsed.(*rsa.PrivateKey)
   if !ok{err=errors.New("RSA signing key required")}
  }
 }else{err=errors.New("unsupported key encoding")}
 if err!=nil{log.Fatal("invalid signing key")}
 secret,err:=credential(os.Getenv("HOMELAB_IDENTITY_SECRET_FILE"),256)
 if err!=nil{log.Fatal(err)}
 handler,err:=machineidentity.NewHandler(machineidentity.Config{
  Issuer:os.Getenv("HOMELAB_IDENTITY_ISSUER"),
  Audience:os.Getenv("HOMELAB_IDENTITY_AUDIENCE"),
  ClientID:os.Getenv("HOMELAB_IDENTITY_CLIENT_ID"),
  PrivateKey:signingKey,ClientSecret:secret,
 })
 for i:=range secret{secret[i]=0}
 if err!=nil{log.Fatal(err)}
 listener,err:=net.Listen("tcp4","127.0.0.1:8182")
 if err!=nil{log.Fatal(err)}
 defer listener.Close()
 srv:=&http.Server{Handler:handler,ReadHeaderTimeout:5*time.Second,
  ReadTimeout:10*time.Second,WriteTimeout:10*time.Second,
  IdleTimeout:30*time.Second,MaxHeaderBytes:8192}
 ctx,stop:=signal.NotifyContext(context.Background(),syscall.SIGTERM,syscall.SIGINT)
 defer stop()
 go func(){
  <-ctx.Done()
  shutdown,cancel:=context.WithTimeout(context.Background(),10*time.Second)
  defer cancel()
  _=srv.Shutdown(shutdown)
 }()
 if err:=srv.Serve(listener);err!=nil&&!errors.Is(err,http.ErrServerClosed){log.Fatal(err)}
}
