package security

import (
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "net/url"
 "regexp"
 "strings"
 "testing"
 "time"
)

func TestOwnerRequestRequiresSignedGatewayPrincipalAndCurrentOwnerSession(t *testing.T) {
 manager,key,now:=testSessionManager(t)
 verifier,assertion,principal:=accessFixture(t,"gateway-aud")
 if err:=manager.SetAccessVerifier(verifier,"gateway-aud");err!=nil{t.Fatal(err)}
 handler:=manager.ClientHandler()
 call:=func(token,payload string)*httptest.ResponseRecorder {
  r:=httptest.NewRequest(http.MethodPost,"http://unix/v1/login-requests",strings.NewReader(payload))
  r.Header.Set("Content-Type","application/json")
  r.Header.Set("Cf-Access-Jwt-Assertion",token)
  w:=httptest.NewRecorder()
  handler.ServeHTTP(w,r)
  return w
 }
 if r:=call("","{}");r.Code!=http.StatusForbidden {t.Fatal("unsigned gateway request accepted")}
 if r:=call(assertion,`{"principal":"somebody"}`);r.Code!=http.StatusBadRequest{t.Fatal("caller-controlled principal accepted")}
 r:=call(assertion,"{}")
 if r.Code!=http.StatusOK{t.Fatal("owner request not issued",r.Code)}
 var request OwnerRequest
 if err:=json.Unmarshal(r.Body.Bytes(),&request);err!=nil{t.Fatal(err)}
 if len(request.RequestID)!=48||request.ReferenceCode!=request.RequestID[:12]||request.ExpiresInSeconds!=300 {
  t.Fatal("invalid public owner request reference")
 }
 if _,ok:=manager.OwnerRequestStatusFor("another-principal",request.RequestID);ok{t.Fatal("owner request leaked")}
 if manager.ApproveOwnerRequestFor(principal,request.RequestID){t.Fatal("unauthenticated approval accepted")}
 if _,err:=manager.AuthenticateFor(principal,testOwnerPassword,TOTPCode(key,now.Unix()/30));err!=nil{t.Fatal(err)}
 if !manager.ApproveOwnerRequestFor(principal,request.RequestID){t.Fatal("valid owner approval refused")}
 if manager.ApproveOwnerRequestFor(principal,request.RequestID){t.Fatal("owner approval replayed")}
 if completed,ok:=manager.OwnerRequestStatusFor(principal,request.RequestID);!ok||!completed{t.Fatal("approval not recorded")}
 manager.RevokeFor(principal)
 if manager.ApproveOwnerRequestFor(principal,request.RequestID){t.Fatal("revoked owner session approved request")}
}

func TestOwnerRequestExpiryAndCapacity(t *testing.T) {
 manager,_,now:=testSessionManager(t)
 var first OwnerRequest
 for i:=0;i<maxOwnerRequests;i++{
  request,err:=manager.OpenOwnerRequestFor("principal")
  if err!=nil{t.Fatal(err)}
  if i==0{first=request}
 }
 if _,err:=manager.OpenOwnerRequestFor("principal");err==nil{t.Fatal("unbounded owner requests")}
 *now=now.Add(ownerRequestLifetime)
 if _,ok:=manager.OwnerRequestStatusFor("principal",first.RequestID);ok{t.Fatal("expired owner request retained")}
 if _,err:=manager.OpenOwnerRequestFor("principal");err!=nil{t.Fatal("capacity not reclaimed",err)}
 if _,ok:=manager.OwnerRequestStatusFor("principal",strings.Repeat("F",48));ok{t.Fatal("noncanonical request accepted")}
}

func TestBrowserRequestRequiresOwnerFactorsAndSingleUseCSRF(t *testing.T) {
 manager,key,now:=testSessionManager(t)
 verifier,assertion,principal:=accessFixture(t,"browser-aud")
 portal,err:=NewBrowserPortal(manager,"https://auth.example.test",func(token string)(string,error){
  return verifier.Verify(token,"browser-aud")
 })
 if err!=nil{t.Fatal(err)}
 request,err:=manager.OpenOwnerRequestFor(principal)
 if err!=nil{t.Fatal(err)}
 get:=func(token,id string)*httptest.ResponseRecorder{
  r:=httptest.NewRequest(http.MethodGet,"https://auth.example.test/request/"+id,nil)
  r.Header.Set("Cf-Access-Jwt-Assertion",token)
  w:=httptest.NewRecorder()
  portal.ServeHTTP(w,r)
  return w
 }
 if r:=get("",request.RequestID);r.Code!=http.StatusForbidden{t.Fatal("browser accepted unsigned assertion")}
 if r:=get(assertion,strings.Repeat("a",48));r.Code!=http.StatusNotFound{t.Fatal("unissued request accepted")}
 page:=get(assertion,request.RequestID)
 if page.Code!=http.StatusOK||!strings.Contains(page.Body.String(),request.ReferenceCode)||
  !strings.Contains(page.Body.String(),"Authenticate and approve request"){t.Fatal("request-specific login unavailable",page.Code)}
 csrf:=regexp.MustCompile(`name="csrf" value="([a-f0-9]{48})"`).FindStringSubmatch(page.Body.String())
 if len(csrf)!=2{t.Fatal("CSRF missing")}
 form:=url.Values{"csrf":{csrf[1]},"password":{testOwnerPassword},"totp":{TOTPCode(key,now.Unix()/30)}}
 post:=func(origin string)*httptest.ResponseRecorder{
  r:=httptest.NewRequest(http.MethodPost,"https://auth.example.test/request/"+request.RequestID+"/login",strings.NewReader(form.Encode()))
  r.Header.Set("Cf-Access-Jwt-Assertion",assertion)
  r.Header.Set("Content-Type","application/x-www-form-urlencoded")
  r.Header.Set("Origin",origin)
  r.Header.Set("Sec-Fetch-Site","same-origin")
  w:=httptest.NewRecorder()
  portal.ServeHTTP(w,r)
  return w
 }
 if r:=post("https://evil.example.test");r.Code!=http.StatusForbidden{t.Fatal("cross-origin login accepted")}
 success:=post("https://auth.example.test")
 if success.Code!=http.StatusSeeOther ||success.Header().Get("Location")!="/request/"+request.RequestID{
  t.Fatal("request login failed",success.Code)
 }
 if r:=post("https://auth.example.test");r.Code!=http.StatusForbidden{t.Fatal("CSRF replay accepted")}
 after:=get(assertion,request.RequestID)
 if after.Code!=http.StatusOK||!strings.Contains(after.Body.String(),"Owner request approved"){t.Fatal("approved request not displayed")}
 if manager.ApproveOwnerRequestFor(principal,request.RequestID){t.Fatal("request approved twice")}
}
