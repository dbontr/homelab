package transport

import (
 "net/http/httptest"
 "path/filepath"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/store"
)

func TestGatewayRejectsUntrustedBrowserOrigins(t *testing.T) {
 db,err:=store.Open(filepath.Join(t.TempDir(),"state.json"),"UTC")
 if err!=nil{t.Fatal(err)}
 s,err:=New(db,strings.Repeat("s",40),nil);if err!=nil{t.Fatal(err)}
 h:=s.Handler()
 send:=func(origin,host string)int{
  req:=httptest.NewRequest("GET","http://127.0.0.1:8300/v1/tasks",nil)
  req.Header.Set("Authorization","Bearer "+strings.Repeat("s",40))
  if origin!="" {req.Header.Set("Origin",origin)}
  if host!="" {req.Host=host}
  w:=httptest.NewRecorder();h.ServeHTTP(w,req)
  return w.Code
 }
 if n:=send("http://attacker.example","127.0.0.1:8300");n!=403{t.Fatalf("hostile origin: %d",n)}
 if n:=send("null","127.0.0.1:8300");n!=403{t.Fatalf("null origin: %d",n)}
 if n:=send("http://attacker.example","attacker.example");n!=403{t.Fatalf("rebound host accepted: %d",n)}
 if n:=send("http://127.0.0.1:8300","127.0.0.1:8300");n!=200{t.Fatalf("local origin rejected: %d",n)}
 if n:=send("","127.0.0.1:8300");n!=200{t.Fatalf("non-browser client rejected: %d",n)}
 if err:=s.SetAllowedOrigins([]string{"https://app.example"});err!=nil{t.Fatal(err)}
 if n:=send("https://app.example","127.0.0.1:8300");n!=200{t.Fatalf("allowlisted origin rejected: %d",n)}
}

func TestGatewayOriginConfigurationRejectsWildcardsAndCredentials(t *testing.T){
 db,err:=store.Open(filepath.Join(t.TempDir(),"state.json"),"UTC");if err!=nil{t.Fatal(err)}
 s,err:=New(db,strings.Repeat("a",40),nil);if err!=nil{t.Fatal(err)}
 for _,origin:=range []string{"*","null","http://public.example","https://user:pass@example.test","https://app.example/path","https://app.example?token=x","https://app.example:0","https://app.example,https://evil.example"}{
  if err:=s.SetAllowedOrigins([]string{origin});err==nil{t.Fatalf("accepted invalid origin: %q",origin)}
 }
}
