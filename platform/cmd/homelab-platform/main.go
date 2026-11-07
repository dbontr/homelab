package main

import (
    "errors"
    "fmt"
    "log"
    "net"
    "net/http"
    "os"
    "strings"
    "time"

    "github.com/dbontr/homelab-new/platform/store"
    "github.com/dbontr/homelab-new/platform/transport"
)

func secret(path string) (string,error) {
    if path=="" {return "",errors.New("token file path is required")}
    b,e:=os.ReadFile(path);if e!=nil{return "",e}
    v:=strings.TrimSpace(string(b));if len(v)<32{return "",errors.New("token must be at least 32 characters")}
    return v,nil
}

func configuredBackend(name, upper string)(transport.Backend,bool,error){
    u:=os.Getenv("HOMELAB_"+upper+"_MCP_URL");if u==""{return transport.Backend{},false,nil}
    path:=os.Getenv("HOMELAB_"+upper+"_MCP_TOKEN_FILE")
    token:="";if path!=""{var e error;token,e=secret(path);if e!=nil{return transport.Backend{},false,e}}
    return transport.Backend{Name:name,URL:u,Token:token},true,nil
}

func main(){
    token,e:=secret(os.Getenv("HOMELAB_API_TOKEN_FILE"));if e!=nil{log.Fatal(e)}
    state:=os.Getenv("HOMELAB_STATE_FILE");if state==""{state="/var/lib/homelab-platform/state.json"}
    db,e:=store.Open(state,os.Getenv("HOMELAB_TIME_ZONE"));if e!=nil{log.Fatal(e)}
    backends:=[]transport.Backend{}
    for _,b:=range []struct{name,upper string}{{"workspace","WORKSPACE"},{"learning","LEARNING"},{"health","HEALTH"}}{
        v,enabled,e:=configuredBackend(b.name,b.upper);if e!=nil{log.Fatal(e)};if enabled{backends=append(backends,v)}
    }
    server,e:=transport.New(db,token,backends);if e!=nil{log.Fatal(e)}
    if file:=os.Getenv("HOMELAB_CALENDAR_URL_FILE");file!=""{
        raw,e:=os.ReadFile(file);if e!=nil{log.Fatal(e)}
        if e:=server.SetCalendarURL(strings.TrimSpace(string(raw)));e!=nil{log.Fatal(e)}
    }
    addr:=os.Getenv("HOMELAB_LISTEN_ADDR");if addr==""{addr="127.0.0.1:8300"}
    host,_,e:=net.SplitHostPort(addr);if e!=nil{log.Fatal(e)}
    if host!="127.0.0.1"&&host!="::1"&&host!="localhost"{log.Fatal("homelab-platform must listen on loopback behind an authenticated ingress")}
    srv:=&http.Server{Addr:addr,Handler:server.Handler(),ReadHeaderTimeout:10*time.Second,ReadTimeout:30*time.Second,WriteTimeout:180*time.Second,IdleTimeout:120*time.Second}
    log.Print(fmt.Sprintf("homelab-platform listening on %s",addr))
    log.Fatal(srv.ListenAndServe())
}
