package main

import (
    "context"
    "errors"
    "fmt"
    "log"
    "net"
    "net/http"
    "os"
    "path/filepath"
    "strings"
    "time"

    "github.com/dbontr/homelab-new/system/agentflow"
    "github.com/dbontr/homelab-new/system/books"
    "github.com/dbontr/homelab-new/system/internal/calendarhttp"
    "github.com/dbontr/homelab-new/system/devices"
    "github.com/dbontr/homelab-new/system/media"
    "github.com/dbontr/homelab-new/system/observability"
    "github.com/dbontr/homelab-new/system/profiles"
    "github.com/dbontr/homelab-new/system/recipes"
    "github.com/dbontr/homelab-new/system/store"
    "github.com/dbontr/homelab-new/system/internal/stateowner"
    "github.com/dbontr/homelab-new/system/security"
    "github.com/dbontr/homelab-new/system/privileged"
    "github.com/dbontr/homelab-new/system/release"
    "github.com/dbontr/homelab-new/system/session"
    "encoding/json"
    "github.com/dbontr/homelab-new/system/transport"
)

func secret(path string) (string,error) {
    if path=="" {return "",errors.New("token file path is required")}
    b,e:=os.ReadFile(path);if e!=nil{return "",e}
    v:=strings.TrimSpace(string(b));if len(v)<32{return "",errors.New("token must be at least 32 characters")}
    return v,nil
}

func configuredBackend(name,upper string,vault *security.RemoteAuthorizationClient)(transport.Backend,bool,error){
 u:=os.Getenv("HOMELAB_"+upper+"_MCP_URL")
 path:=os.Getenv("HOMELAB_"+upper+"_MCP_TOKEN_FILE")
 alias:=os.Getenv("HOMELAB_"+upper+"_MCP_VAULT_ALIAS")
 if u==""{
  if path!=""||alias!=""{return transport.Backend{},false,errors.New("backend credential without endpoint")}
  return transport.Backend{},false,nil
 }
 if path!=""&&alias!=""{return transport.Backend{},false,errors.New("backend has conflicting credential authorities")}
 var source func()(string,error)
 var err error
 if alias!=""{
  if vault==nil{return transport.Backend{},false,errors.New("backend vault is unavailable")}
  source,err=vault.TokenSource(alias)
  if err!=nil{return transport.Backend{},false,err}
 }
 token:=""
 if path!=""{token,err=secret(path);if err!=nil{return transport.Backend{},false,err}}
 return transport.Backend{Name:name,URL:u,Token:token,TokenSource:source},true,nil
}

func main(){
    token,e:=secret(os.Getenv("HOMELAB_API_TOKEN_FILE"));if e!=nil{log.Fatal(e)}
    state:=os.Getenv("HOMELAB_STATE_FILE");if state==""{state="/var/lib/homelab-platform/state.json"}
    profileFile:=os.Getenv("HOMELAB_PROFILES_FILE")
    if profileFile==""{profileFile=filepath.Join(filepath.Dir(state),"profiles.json")}
    recipeFile:=os.Getenv("HOMELAB_RECIPES_FILE")
    if recipeFile==""{recipeFile=filepath.Join(filepath.Dir(state),"recipes.json")}
    workflowDir:=filepath.Join(filepath.Dir(state),"workflows")
    ownership,e:=stateowner.Acquire(state,profileFile,recipeFile,workflowDir)
    if e!=nil{log.Fatal("state ownership: ",e)}
    defer ownership.Close()
    db,e:=store.Open(state,os.Getenv("HOMELAB_TIME_ZONE"));if e!=nil{log.Fatal(e)}
    vault,err:=security.NewVaultClientFromEnv("HOMELAB")
    if err!=nil{log.Fatal("service credential trust: ",err)}
    backends:=[]transport.Backend{}
    for _,b:=range []struct{name,upper string}{{"workspace","WORKSPACE"},{"learning","LEARNING"},{"health","HEALTH"}}{
        v,enabled,e:=configuredBackend(b.name,b.upper,vault);if e!=nil{log.Fatal(e)};if enabled{backends=append(backends,v)}
    }
    server,e:=transport.New(db,token,backends);if e!=nil{log.Fatal(e)}
    mcpInterfaceToken,apiInterfaceToken:="",""
    if path:=os.Getenv("HOMELAB_MCP_INTERFACE_TOKEN_FILE");path!=""{
        mcpInterfaceToken,e=secret(path)
        if e!=nil{log.Fatal("MCP service identity: ",e)}
    }
    if path:=os.Getenv("HOMELAB_API_INTERFACE_TOKEN_FILE");path!=""{
        apiInterfaceToken,e=secret(path)
        if e!=nil{log.Fatal("API service identity: ",e)}
    }
    var apiWritePaths []string
    if raw:=os.Getenv("HOMELAB_API_INTERFACE_WRITE_PATHS");raw!=""{
        apiWritePaths=strings.Split(raw,",")
    }
    if e:=server.SetInterfaceTokens(mcpInterfaceToken,apiInterfaceToken,apiWritePaths);e!=nil{
        log.Fatal("interface service policy: ",e)
    }
    workflows,err:=agentflow.Open(workflowDir)
    if err!=nil{log.Fatal(err)}
    server.SetWorkflows(workflows)
    planner,err:=configuredCapabilityPlanner(vault)
    if err!=nil{log.Fatal("capability model configuration: ",err)}
    if planner!=nil{server.SetCapabilityPlanner(planner)}
    server.StartWorkflowScheduler(context.Background())
    if socket:=os.Getenv("HOMELAB_ENGINEERING_SOCKET");socket!=""{
        client,err:=agentflow.NewClient(socket)
        if err!=nil{log.Fatal(err)}
        server.SetEngineering(client)
    }
    prom,loki:=os.Getenv("HOMELAB_PROMETHEUS_URL"),os.Getenv("HOMELAB_LOKI_URL")
    if prom!=""||loki!=""{
        monitoring,err:=observability.New(prom,loki)
        if err!=nil{log.Fatal(err)}
        server.SetObservability(monitoring)
    }
    if path:=os.Getenv("HOMELAB_BOOKS_CONTACT_FILE");path!=""{
        contact,e:=os.ReadFile(path);if e!=nil{log.Fatal(e)}
        catalog,e:=books.New(strings.TrimSpace(string(contact)));if e!=nil{log.Fatal(e)}
        server.SetBookCatalog(catalog)
    }
    if raw:=os.Getenv("HOMELAB_ALLOWED_ORIGINS");raw!=""{
        if err:=server.SetAllowedOrigins(strings.Split(raw,","));err!=nil{log.Fatal(err)}
    }
    registry,e:=profiles.Open(profileFile);if e!=nil{log.Fatal(e)}
    server.SetProfiles(registry)
    recipeStore,e:=recipes.Open(recipeFile);if e!=nil{log.Fatal(e)}
    server.SetRecipes(recipeStore)
    if file:=os.Getenv("HOMELAB_CONSOLE_PASSPHRASE_FILE");file!=""{
        b,e:=os.ReadFile(file);if e!=nil{log.Fatal(e)}
        var record security.PassphraseRecord
        if e:=json.Unmarshal(b,&record);e!=nil{log.Fatal(e)}
        manager,e:=session.New(record);if e!=nil{log.Fatal(e)}
        server.SetSessions(manager)
    }
    if file:=os.Getenv("HOMELAB_DEVICES_FILE");file!=""{
        catalog,e:=devices.Load(file);if e!=nil{log.Fatal(e)}
        server.SetDeviceCatalog(catalog)
    }
    if file:=os.Getenv("HOMELAB_NODES_FILE");file!="" {
        nodes,err:=devices.LoadNodes(file);if err!=nil {log.Fatal(err)}
        server.SetNodeCatalog(nodes)
        if catalogFile:=os.Getenv("HOMELAB_DEVICES_FILE");catalogFile!="" {
            catalog,err:=devices.Load(catalogFile);if err!=nil {log.Fatal(err)}
            if err:=catalog.ValidateNodes(nodes);err!=nil {log.Fatal(err)}
        }
    }
    if url:=os.Getenv("HOMELAB_SEERR_URL");url!=""{
        path:=os.Getenv("HOMELAB_SEERR_API_KEY_FILE")
        alias:=os.Getenv("HOMELAB_SEERR_VAULT_ALIAS")
        if (path=="")==(alias==""){log.Fatal("Seerr requires exactly one credential authority")}
        var client *media.Client
        if alias!=""{
            if vault==nil{log.Fatal("Seerr vault is unavailable")}
            source,err:=vault.TokenSource(alias)
            if err!=nil{log.Fatal(err)}
            client,err=media.NewSeerrWithSource(url,source)
            if err!=nil{log.Fatal(err)}
        }else{
            raw,e:=os.ReadFile(path);if e!=nil{log.Fatal(e)}
            client,e=media.NewSeerr(url,strings.TrimSpace(string(raw)));if e!=nil{log.Fatal(e)}
        }
        server.ConfigureMedia(client)
    }else if os.Getenv("HOMELAB_SEERR_API_KEY_FILE")!=""||os.Getenv("HOMELAB_SEERR_VAULT_ALIAS")!=""{
        log.Fatal("Seerr credential requires endpoint")
    }
    if endpoint:=os.Getenv("HOMELAB_CALENDAR_SERVICE_URL");endpoint!=""{
        if os.Getenv("HOMELAB_CALENDAR_URL_FILE")!=""{
            log.Fatal("calendar service and local calendar feed must not be configured together")
        }
        token,e:=secret(os.Getenv("HOMELAB_CALENDAR_SERVICE_TOKEN_FILE"))
        if e!=nil{log.Fatal("calendar service identity: ",e)}
        client,e:=calendarhttp.NewClient(endpoint,token)
        if e!=nil{log.Fatal("calendar service connection: ",e)}
        if e:=server.SetCalendarService(client.Fetch);e!=nil{log.Fatal(e)}
    }else if os.Getenv("HOMELAB_CALENDAR_SERVICE_TOKEN_FILE")!=""{
        log.Fatal("calendar service credential requires an endpoint")
    }else if file:=os.Getenv("HOMELAB_CALENDAR_URL_FILE");file!=""{
        raw,e:=os.ReadFile(file);if e!=nil{log.Fatal(e)}
        if e:=server.SetCalendarURL(strings.TrimSpace(string(raw)));e!=nil{log.Fatal(e)}
    }
    if socket:=os.Getenv("HOMELAB_SECURITY_SOCKET");socket!=""{
        client,err:=security.NewAdminClient(socket)
        if err!=nil{log.Fatal(err)}
        server.SetPrivilegeClient(client)
        if raw:=os.Getenv("HOMELAB_SECURITY_LOGIN_URL");raw!=""{
            if err:=server.SetSecurityLoginURL(raw);err!=nil{log.Fatal(err)}
        }
    }else{
        serveApprovals(server,os.Getenv("HOMELAB_APPROVAL_SOCKET"))
    }
    if socket:=os.Getenv("HOMELAB_EXECUTOR_SOCKET");socket!=""{
        if os.Getenv("HOMELAB_SECURITY_SOCKET")==""{log.Fatal("executor requires independent owner authorization")}
        client,err:=privileged.NewClient(socket);if err!=nil{log.Fatal(err)}
        server.SetExecutorClient(client)
    }
    if socket:=os.Getenv("HOMELAB_FLEET_SOCKET");socket!=""{
        if os.Getenv("HOMELAB_SECURITY_SOCKET")==""{log.Fatal("fleet requires independent owner authorization")}
        client,err:=release.NewFleetClient(socket);if err!=nil{log.Fatal(err)}
        server.SetFleetClient(client)
    }
    addr:=os.Getenv("HOMELAB_LISTEN_ADDR");if addr==""{addr="127.0.0.1:8300"}
    host,_,e:=net.SplitHostPort(addr);if e!=nil{log.Fatal(e)}
    if host!="127.0.0.1"&&host!="::1"&&host!="localhost"{log.Fatal("homelab-platform must listen on loopback behind an authenticated ingress")}
    srv:=&http.Server{Addr:addr,Handler:server.Handler(),ReadHeaderTimeout:10*time.Second,ReadTimeout:30*time.Second,WriteTimeout:180*time.Second,IdleTimeout:120*time.Second}
    log.Print(fmt.Sprintf("homelab-platform listening on %s",addr))
    log.Fatal(srv.ListenAndServe())
}
