package security

import (
 "net/http"
 "net/http/httptest"
 "net/url"
 "regexp"
 "strings"
 "testing"
)

func TestBrowserPortalDirectAuthenticationAndRevocation(t *testing.T){
 manager,key,now:=testSessionManager(t)
 portal,err:=NewBrowserPortal(manager,"https://auth.example.test")
 if err!=nil{t.Fatal(err)}
 get:=func(host string)*httptest.ResponseRecorder{
  r:=httptest.NewRequest(http.MethodGet,"https://auth.example.test/",nil)
  r.Host=host
  out:=httptest.NewRecorder()
  portal.ServeHTTP(out,r)
  return out
 }
 if out:=get("other.example.test");out.Code!=http.StatusMisdirectedRequest{t.Fatal("Host validation failed")}
 page:=get("auth.example.test")
 if page.Code!=200||!strings.Contains(page.Body.String(),"Unlock administration"){t.Fatal("login form missing")}
 if !strings.Contains(page.Header().Get("Content-Security-Policy"),"frame-ancestors 'none'"){t.Fatal("frame protection missing")}
 csrf:=regexp.MustCompile(`name="csrf" value="([a-f0-9]{48})"`)
 token:=csrf.FindStringSubmatch(page.Body.String())[1]
 code:=TOTPCode(key,now.Unix()/30)
 post:=func(path,origin,token string,fields url.Values)*httptest.ResponseRecorder{
  fields.Set("csrf",token)
  req:=httptest.NewRequest(http.MethodPost,"https://auth.example.test"+path,strings.NewReader(fields.Encode()))
  req.Header.Set("Content-Type","application/x-www-form-urlencoded")
  req.Header.Set("Origin",origin)
  req.Header.Set("Sec-Fetch-Site","same-origin")
  out:=httptest.NewRecorder()
  portal.ServeHTTP(out,req)
  return out
 }
 fields:=url.Values{"password":{testOwnerPassword},"totp":{code}}
 if out:=post("/login","https://evil.example.test",token,fields);out.Code!=403{t.Fatal("cross-origin login accepted")}
 if out:=post("/login","https://auth.example.test",token,fields);out.Code!=303{t.Fatal("owner login rejected",out.Code)}
 if out:=post("/login","https://auth.example.test",token,fields);out.Code!=403{t.Fatal("CSRF replay accepted")}
 signed:=get("auth.example.test")
 if !strings.Contains(signed.Body.String(),"Administrative session active"){t.Fatal("active session not displayed")}
 if strings.Contains(signed.Body.String(),code)||strings.Contains(signed.Body.String(),testOwnerPassword){t.Fatal("secret leaked")}
 token=csrf.FindStringSubmatch(signed.Body.String())[1]
 if out:=post("/close","https://auth.example.test",token,url.Values{});out.Code!=303||manager.Status().Active{t.Fatal("revocation failed")}
}

func TestBrowserPortalRejectsUnsafeOriginAndExpiry(t *testing.T){
 manager,key,now:=testSessionManager(t)
 for _,origin:=range []string{"http://auth.example.test","https://auth.example.test/path","https://user:pass@auth.example.test","https://127.0.0.1","https://auth.example.test/?next=x"}{
  if _,err:=NewBrowserPortal(manager,origin);err==nil{t.Fatal("unsafe origin allowed",origin)}
 }
 portal,_:=NewBrowserPortal(manager,"https://auth.example.test")
 get:=httptest.NewRequest(http.MethodGet,"https://auth.example.test/",nil)
 page:=httptest.NewRecorder()
 portal.ServeHTTP(page,get)
 csrf:=regexp.MustCompile(`name="csrf" value="([a-f0-9]{48})"`).FindStringSubmatch(page.Body.String())[1]
 payload:=url.Values{"csrf":{csrf},"password":{testOwnerPassword},"totp":{TOTPCode(key,now.Unix()/30)}}
 login:=httptest.NewRequest(http.MethodPost,"https://auth.example.test/login",strings.NewReader(payload.Encode()))
 login.Header.Set("Origin","https://auth.example.test")
 login.Header.Set("Content-Type","application/x-www-form-urlencoded")
 out:=httptest.NewRecorder()
 portal.ServeHTTP(out,login)
 if out.Code!=303||out.Header().Get("Location")!="/"{t.Fatal("unsafe redirect or failed login")}
 *now=now.Add(12*time.Hour)
 if manager.Status().Active{t.Fatal("absolute lifetime exceeded")}
}
