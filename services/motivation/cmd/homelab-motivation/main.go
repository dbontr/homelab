package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/dbontr/homelab-new/system/motivation/internal/motivation"
)

func privateCredential(file string)(string,error){
	if !filepath.IsAbs(file){return "",errors.New("token file path must be absolute")}
	f,err:=os.OpenFile(file,os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC,0)
	if err!=nil{return "",errors.New("private service token unavailable")}
	defer f.Close()
	info,err:=f.Stat()
	if err!=nil{return "",err}
	if !info.Mode().IsRegular()||info.Size()>4096{return "",errors.New("invalid private token descriptor")}
	mode:=info.Mode().Perm()
	allowed:=mode&0077==0
	if !allowed {
		dir:=os.Getenv("CREDENTIALS_DIRECTORY")
		allowed=dir!=""&&filepath.Dir(file)==dir&&mode&0037==0
	}
	if !allowed{return "",errors.New("service token has unsafe permissions")}
	data,err:=io.ReadAll(io.LimitReader(f,4097))
	if err!=nil||len(data)>4096{return "",errors.New("service token exceeds bounds")}
	token:=strings.TrimSpace(string(data))
	if len(token)<32||len(token)>4096{return "",errors.New("invalid service token")}
	for _,c:=range token{
		if c<=32||c==127{return "",errors.New("invalid service token")}
	}
	return token,nil
}

func main() {
	log.SetFlags(log.LstdFlags|log.LUTC)
	token,err:=privateCredential(os.Getenv("HOMELAB_MOTIVATION_TOKEN_FILE"))
	if err!=nil{log.Fatal(err)}
	stateFile:=os.Getenv("HOMELAB_MOTIVATION_STATE_FILE")
	if stateFile=="" {
		directory:=os.Getenv("STATE_DIRECTORY")
		if directory==""{directory="/var/lib/homelab-motivation"}
		stateFile=filepath.Join(directory,"state.json")
	}
	engine,err:=motivation.Open(stateFile)
	if err!=nil{log.Fatal(err)}
	defer engine.Close()
	addr:=os.Getenv("HOMELAB_MOTIVATION_LISTEN_ADDR")
	if addr==""{addr="127.0.0.1:8341"}
	host,port,err:=net.SplitHostPort(addr)
	if err!=nil{log.Fatal(err)}
	ip:=net.ParseIP(host)
	if ip==nil||!ip.IsLoopback()||port==""{
		log.Fatal("motivation service must listen on an explicit loopback IP")
	}
	handler,err:=motivation.NewHandler(engine,token)
	if err!=nil{log.Fatal(err)}
	server:=&http.Server{
		Addr:addr,Handler:handler,
		ReadHeaderTimeout:5*time.Second,
		IdleTimeout:30*time.Second,
		MaxHeaderBytes:8<<10,
	}
	ctx,stop:=signal.NotifyContext(context.Background(),syscall.SIGINT,syscall.SIGTERM)
	defer stop()
	listener,err:=net.Listen("tcp",addr)
	if err!=nil{log.Fatal(err)}
	go func(){
		ticker:=time.NewTicker(60*time.Second)
		defer ticker.Stop()
		for{
			select{
			case <-ctx.Done():return
			case <-ticker.C:
				if _,err:=engine.Tick(motivation.Now());err!=nil{
					log.Printf("motivation scheduler: %v",err)
				}
			}
		}
	}()
	go func(){
		<-ctx.Done()
		cleanup,cancel:=context.WithTimeout(context.Background(),10*time.Second)
		defer cancel()
		if err:=server.Shutdown(cleanup);err!=nil{
			log.Printf("motivation shutdown: %v",err)
		}
	}()
	if err:=server.Serve(listener);err!=nil&&!errors.Is(err,http.ErrServerClosed) {
		log.Fatal(fmt.Errorf("motivation HTTP service failed: %w",err))
	}
}
