package transport

import (
 "context"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "strings"
 "testing"
 "time"

 "github.com/dbontr/homelab-new/system/store"
)

func discoveryBackend(t *testing.T,name,description string) *httptest.Server {
 t.Helper()
 return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  var message struct{Method string `json:"method"`}
  if err:=json.NewDecoder(r.Body).Decode(&message);err!=nil{
   t.Errorf("invalid discovery call: %v",err);return
  }
  if message.Method!="tools/list"{
   t.Errorf("backend received unexpected method: %s",message.Method);return
  }
  w.Header().Set("Content-Type","application/json")
  reply:=map[string]any{
   "jsonrpc":"2.0","id":1,
   "result":map[string]any{"tools":[]any{
    map[string]any{
     "name":name,"description":description,
     "inputSchema":map[string]any{"type":"object","properties":map[string]any{}},
    },
   }},
  }
  if err:=json.NewEncoder(w).Encode(reply);err!=nil{t.Errorf("reply: %v",err)}
 }))
}
func discoveryServer(t *testing.T,backends []Backend)*Server{
 t.Helper()
 db,err:=store.Open(filepath.Join(t.TempDir(),"state.json"),"UTC")
 if err!=nil{t.Fatal(err)}
 gateway,err:=New(db,strings.Repeat("a",40),backends)
 if err!=nil{t.Fatal(err)}
 return gateway
}
func namesInDiscovery(list []map[string]any)([]string,map[string]int){
 names:=[]string{}
 seen:=map[string]int{}
 for _,entry:=range list{
  name,ok:=entry["name"].(string)
  if !ok{continue}
  names=append(names,name)
  seen[name]++
 }
 return names,seen
}
func TestToolDiscoveryKeepsHealthyLocalAndRemoteToolsDuringBackendOutage(t *testing.T){
 fast:=discoveryBackend(t,"health_food_search","USDA lookup")
 defer fast.Close()
 releaseBlocked := make(chan struct{})
 blocked:=httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter,r *http.Request){
  select {
  case <-r.Context().Done():
  case <-releaseBlocked:
  }
 }))
 defer func() { close(releaseBlocked); blocked.Close() }()
 gateway:=discoveryServer(t,[]Backend{
  {Name:"health",URL:fast.URL},
  {Name:"learning",URL:blocked.URL},
 })
 ctx,cancel:=context.WithTimeout(context.Background(),160*time.Millisecond)
 defer cancel()
 request:=httptest.NewRequest(http.MethodPost,"/mcp",nil).WithContext(ctx)
 started:=time.Now()
 tools:=gateway.toolList(request)
 if elapsed:=time.Since(started);elapsed>600*time.Millisecond{
  t.Fatalf("unavailable backend extended discovery beyond one request budget: %v",elapsed)
 }
 names,counts:=namesInDiscovery(tools)
 if counts["task_add"]!=1||counts["health__health_food_search"]!=1{
  t.Fatalf("outage suppressed healthy local or federated tools: %v",names)
 }
 if counts["learning__missing"]!=0{t.Fatal("unavailable integration advertised capabilities")}
}

func TestToolDiscoveryNamespacesStableAndRemovesDuplicateNames(t *testing.T){
 first:=discoveryBackend(t,"pool_list_images","original Workspace provider")
 defer first.Close()
 second:=discoveryBackend(t,"pool_list_images","duplicate compute namespace")
 defer second.Close()
 third:=discoveryBackend(t,"get_learning_session","learning session lookup")
 defer third.Close()
 gateway:=discoveryServer(t,[]Backend{
  {Name:"workspace",URL:first.URL},
  {Name:"compute",URL:second.URL},
  {Name:"learning",URL:third.URL},
 })
 request:=httptest.NewRequest(http.MethodPost,"/mcp",nil)
 for i:=0;i<3;i++{
  result:=gateway.toolList(request)
  names,counts:=namesInDiscovery(result)
  if counts["compute__pool_list_images"]!=1||counts["learning__get_learning_session"]!=1{
   t.Fatalf("duplicates or missing names: %v",names)
  }
  imageIndex,learningIndex:=-1,-1
  for index,name:=range names{
   if name=="compute__pool_list_images" {imageIndex=index}
   if name=="learning__get_learning_session"{learningIndex=index}
  }
  if imageIndex<0||learningIndex<0||imageIndex>=learningIndex{
   t.Fatalf("backend configuration order was not preserved: %v",names)
  }
  if description,_:=result[imageIndex]["description"].(string);description!="original Workspace provider"{
   t.Fatalf("duplicate namespace changed capability owner: %s",description)
  }
 }
}
