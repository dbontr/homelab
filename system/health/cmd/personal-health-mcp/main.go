package main

import (
 "context"
 "log"
 "os"
 "os/signal"
 "strings"
 "time"
 "syscall"

 "github.com/dbontr/homelab-new/system/health/internal/health"
 "github.com/dbontr/homelab-new/system/health/internal/googlehealth"
 "github.com/dbontr/homelab-new/system/health/internal/fooddata"
 "github.com/dbontr/homelab-new/system/health/internal/mcp"
 "github.com/dbontr/homelab-new/system/health/internal/recipesage"
)
func main() {
 path:=os.Getenv("PERSONAL_HEALTH_DB_PATH")
 if path=="" {path="data/health.db"}
 store,err:=health.Open(path)
 if err!=nil {log.Fatalf("health database: %v",err)}
 defer store.Close()
 if len(os.Args)>1{
  switch os.Args[1]{
  case "backup":
   if len(os.Args)!=3{log.Fatal("usage: personal-health-mcp backup <private-snapshot-path>")}
   if err:=store.Backup(context.Background(),os.Args[2]);err!=nil{log.Fatalf("backup: %v",err)}
   log.Print("SQLite backup completed")
   return
  case "verify":
   if len(os.Args)!=2{log.Fatal("usage: personal-health-mcp verify")}
   if err:=store.IntegrityCheck(context.Background());err!=nil{log.Fatalf("database integrity: %v",err)}
   log.Print("SQLite integrity check passed")
   return
  default:log.Fatal("unknown command; use backup or verify")
  }
 }
 var recipes *recipesage.Client
 if os.Getenv("RECIPE_SAGE_URL")!="" {
  recipes,err=recipesage.New(recipesage.Config{
   URL:os.Getenv("RECIPE_SAGE_URL"),Token:os.Getenv("RECIPE_SAGE_TOKEN"),
   Email:os.Getenv("RECIPE_SAGE_EMAIL"),Password:os.Getenv("RECIPE_SAGE_PASSWORD"),
   AllowHTTP:os.Getenv("RECIPE_SAGE_ALLOW_HTTP")=="true",
  })
  if err!=nil {log.Fatalf("RecipeSage integration: %v",err)}
 }
 server:=mcp.New(recipes,store)
 server.EnableFoodData(fooddata.New(fooddata.Config{USDAKey:os.Getenv("USDA_FDC_API_KEY")}))
 var googleClient *googlehealth.Client
 if os.Getenv("GOOGLE_HEALTH_ACCESS_TOKEN")!="" || os.Getenv("GOOGLE_HEALTH_REFRESH_TOKEN")!="" {
  googleClient,err=googlehealth.New(googlehealth.Config{AccessToken:os.Getenv("GOOGLE_HEALTH_ACCESS_TOKEN"),RefreshToken:os.Getenv("GOOGLE_HEALTH_REFRESH_TOKEN"),ClientID:os.Getenv("GOOGLE_HEALTH_CLIENT_ID"),ClientSecret:os.Getenv("GOOGLE_HEALTH_CLIENT_SECRET")})
  if err!=nil{log.Fatalf("Google Health integration: %v",err)}
  server.EnableGoogle(googleClient)
 }
 ctx,cancel:=signal.NotifyContext(context.Background(),os.Interrupt,syscall.SIGTERM)
 defer cancel()
 if raw:=os.Getenv("GOOGLE_HEALTH_SYNC_INTERVAL");raw!=""{
  if googleClient==nil{log.Fatal("Google Health automatic sync requires authorized Google credentials")}
  interval,err:=time.ParseDuration(raw)
  if err!=nil{log.Fatalf("invalid Google sync interval: %v",err)}
  lookback:=7*24*time.Hour
  if v:=os.Getenv("GOOGLE_HEALTH_SYNC_LOOKBACK");v!=""{
   lookback,err=time.ParseDuration(v)
   if err!=nil{log.Fatalf("invalid Google sync lookback: %v",err)}
  }
  kinds:=[]string{"sleep","steps","weight","heart-rate","daily-resting-heart-rate"}
  if rawTypes:=os.Getenv("GOOGLE_HEALTH_SYNC_TYPES");rawTypes!=""{
   kinds=[]string{}
   for _,kind:=range strings.Split(rawTypes,","){kinds=append(kinds,strings.TrimSpace(kind))}
  }
  cfg:=googlehealth.PollConfig{DataTypes:kinds,Interval:interval,Lookback:lookback,MaxPagesPerType:20}
  if err:=googlehealth.ValidatePollConfig(cfg);err!=nil{log.Fatalf("invalid Google sync configuration: %v",err)}
  go func(){if err:=googleClient.RunPoller(ctx,store,cfg);err!=nil&&ctx.Err()==nil{
   log.Printf("Google Health poller stopped: %v",err)
  }}()
 }
 transport:=os.Getenv("MCP_TRANSPORT")
 switch transport {
 case "","stdio":
  if err:=server.ServeStdio(ctx,os.Stdin,os.Stdout);err!=nil {log.Fatal(err)}
 case "http":
  address:=os.Getenv("MCP_LISTEN")
  if address=="" {address="127.0.0.1:8080"}
  origins:=[]string{}
  for _,value:=range strings.Split(os.Getenv("MCP_ALLOWED_ORIGINS"),",") {
   if s:=strings.TrimSpace(value);s!=""{origins=append(origins,s)}
  }
  access,err:=mcp.AccessVerifierFromEnvironment()
  if err!=nil{log.Fatalf("Cloudflare Access configuration: %v",err)}
  server.Access=access
  if err:=server.ListenHTTP(ctx,address,os.Getenv("MCP_API_TOKEN"),origins);err!=nil{log.Fatal(err)}
 default:log.Fatal("MCP_TRANSPORT must be stdio or http")
 }
}
