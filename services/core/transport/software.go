package transport

import (
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "net/http"
)

// The Workspace pool is the sole source of software and image identities.
func (s *Server) hasBackend(name string) bool {
 for _,b:=range s.backend {if b.Name==name{return true}}
 return false
}

func(s *Server) softwareEntries(ctx context.Context)(map[string]any,error){
 var backend *Backend
 for i:=range s.backend {if s.backend[i].Name=="workspace"{backend=&s.backend[i];break}}
 if backend==nil{return nil,errors.New("software library is not configured")}
 result:=map[string]any{}
 for _,item:=range []struct{key,tool string}{
   {"images","pool_list_images"},
   {"software","pool_list_software"},
   {"programs","pool_list_programs"},
 }{
   params,_:=json.Marshal(map[string]any{"name":item.tool,"arguments":map[string]any{}})
   var response any
   if err:=s.forward(ctx,*backend,rpcRequest{JSONRPC:"2.0",ID:json.RawMessage("1"),Method:"tools/call",Params:params},&response);err!=nil{return nil,err}
   obj,ok:=response.(map[string]any);if !ok{return nil,errors.New("invalid software library response")}
   if failed,_:=obj["isError"].(bool);failed{return nil,fmt.Errorf("software catalog rejected %s",item.tool)}
   contents,ok:=obj["content"].([]any)
   if !ok||len(contents)==0{return nil,errors.New("missing software catalog content")}
   first,ok:=contents[0].(map[string]any)
   if !ok{return nil,errors.New("invalid software catalog content")}
   text,ok:=first["text"].(string)
   if !ok||len(text)>2<<20{return nil,errors.New("software catalog response exceeds limits")}
   var data any
   if err:=json.Unmarshal([]byte(text),&data);err!=nil{return nil,errors.New("software catalog returned invalid JSON")}
   result[item.key]=data
 }
 return result,nil
}

func(s *Server) softwareLibrary(w http.ResponseWriter,r *http.Request){
 entries,err:=s.softwareEntries(r.Context())
 if err!=nil {writeError(w,http.StatusServiceUnavailable,err.Error());return}
 writeJSON(w,http.StatusOK,entries)
}
