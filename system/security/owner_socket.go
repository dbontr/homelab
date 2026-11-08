package security

import (
 "context"
 "encoding/json"
 "errors"
 "io"
 "net"
 "net/http"
 "os"
 "path/filepath"
 "strings"
 "time"
)

// OwnerApprovals runs in an independent operator-owned process, not the MCP gateway.
type OwnerApprovals struct {
 authority *Authority
 unlock *OwnerUnlock
 allowed map[string]bool
}

func NewOwnerApprovals(authority *Authority,unlock *OwnerUnlock,operations []string)(*OwnerApprovals,error){
 if authority==nil||unlock==nil||len(operations)==0{return nil,errors.New("owner approvals require a local authority, verifier and operation list")}
 allowed:=make(map[string]bool,len(operations))
 for _,name:=range operations{
  if name==""||len(name)>128||allowed[name]{return nil,errors.New("invalid privileged operation")}
  allowed[name]=true
 }
 return &OwnerApprovals{authority:authority,unlock:unlock,allowed:allowed},nil
}

func(a *OwnerApprovals)ownerHandler(w http.ResponseWriter,r *http.Request){
 w.Header().Set("Cache-Control","no-store")
 w.Header().Set("X-Content-Type-Options","nosniff")
 if r.Method!=http.MethodPost||r.URL.Path!="/approvals/issue"{http.Error(w,"not found",http.StatusNotFound);return}
 r.Body=http.MaxBytesReader(w,r.Body,16<<10)
 var input struct{
  Passphrase string `json:"passphrase"`
  Operation string `json:"operation"`
  Target string `json:"target"`
  Arguments json.RawMessage `json:"arguments"`
 }
 decoder:=json.NewDecoder(r.Body)
 decoder.DisallowUnknownFields()
 if err:=decoder.Decode(&input);err!=nil{http.Error(w,"invalid request",400);return}
 var trailing any
 if err:=decoder.Decode(&trailing);err!=io.EOF{http.Error(w,"invalid request",400);return}
 if !a.allowed[input.Operation]{http.Error(w,"operation not authorized",403);return}
 if err:=a.unlock.Verify([]byte(input.Passphrase));err!=nil{http.Error(w,"owner unlock denied",401);return}
 input.Passphrase=""
 grant,err:=a.authority.Issue(input.Operation,input.Target,input.Arguments,MaxGrantDuration)
 if err!=nil{http.Error(w,"invalid operation",400);return}
 w.Header().Set("Content-Type","application/json")
 _=json.NewEncoder(w).Encode(map[string]string{"grant":grant})
}

// ServeOwnerSocket is restricted to a private Unix socket; it never binds TCP.
func(a *OwnerApprovals)ServeOwnerSocket(ctx context.Context,path string)error{
 if !filepath.IsAbs(path)||strings.ContainsRune(path,'\x00'){return errors.New("owner socket path must be absolute")}
 dir:=filepath.Dir(path)
 if err:=os.MkdirAll(dir,0700);err!=nil{return err}
 info,err:=os.Lstat(dir)
 if err!=nil||!info.IsDir()||info.Mode().Perm()&0077!=0{return errors.New("owner socket directory must be private")}
 if _,err:=os.Lstat(path);err==nil{return errors.New("owner socket path already exists")
 }else if !errors.Is(err,os.ErrNotExist){return err}
 listener,err:=net.Listen("unix",path)
 if err!=nil{return err}
 defer listener.Close()
 defer os.Remove(path)
 if err:=os.Chmod(path,0600);err!=nil{return err}
 server:=&http.Server{Handler:http.HandlerFunc(a.ownerHandler),ReadHeaderTimeout:5*time.Second}
 done:=make(chan struct{})
 go func(){select{
  case <-ctx.Done():_=server.Close()
  case <-done:
 }}()
 err=server.Serve(listener)
 close(done)
 if errors.Is(err,http.ErrServerClosed){return nil}
 return err
}
