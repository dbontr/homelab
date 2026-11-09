package security

import (
 "bytes"
 "context"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

func TestOAuthRefreshStoresRotatedTokensWithoutExposingThem(t *testing.T){
 calls:=0
 providerServer:=httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  calls++
  if r.Method!=http.MethodPost||r.PostFormValue("grant_type")!="refresh_token"||
   r.PostFormValue("refresh_token")!="old-refresh"||r.PostFormValue("client_id")!="public-client"{
   t.Error("incorrect provider refresh request");w.WriteHeader(http.StatusBadRequest);return
  }
  w.Header().Set("Content-Type","application/json")
  _,_=w.Write([]byte(`{"access_token":"new-access","refresh_token":"new-refresh","token_type":"Bearer","expires_in":3600}`))
 }))
 defer providerServer.Close()
 path:=filepath.Join(t.TempDir(),"vault.json")
 vault,err:=OpenVault(path,bytes.Repeat([]byte("k"),32));if err!=nil{t.Fatal(err)}
 old:=[]byte(`{"access_token":"old-access","refresh_token":"old-refresh","token_type":"Bearer"}`)
 handle,err:=vault.Store("vendor",old);if err!=nil{t.Fatal(err)}
 version,err:=vault.Version(handle.ID,"vendor");if err!=nil{t.Fatal(err)}
 provider:=Provider{Name:"vendor",ClientID:"public-client",TokenURL:providerServer.URL}
 if err:=RefreshOAuthCredential(context.Background(),provider,handle.ID,providerServer.Client(),vault);err!=nil{t.Fatal(err)}
 if calls!=1{t.Fatal("unexpected provider refresh calls",calls)}
 newer,err:=vault.Version(handle.ID,"vendor")
 if err!=nil||newer==version{t.Fatal("refresh did not rotate vault record")}
 reopened,err:=OpenVault(path,bytes.Repeat([]byte("k"),32));if err!=nil{t.Fatal(err)}
 if err:=reopened.WithCredential(handle.ID,"vendor",func(data []byte)error{
  var token struct{
   Access string `json:"access_token"`
   Refresh string `json:"refresh_token"`
  }
  if err:=json.Unmarshal(data,&token);err!=nil{t.Fatal(err)}
  if token.Access!="new-access"||token.Refresh!="new-refresh"{t.Fatal("wrong refreshed secrets")}
  return nil
 });err!=nil{t.Fatal(err)}
 disk,err:=os.ReadFile(path)
 if err!=nil||bytes.Contains(disk,[]byte("new-access"))||bytes.Contains(disk,[]byte("new-refresh")){
  t.Fatal("refresh tokens persisted in plaintext")
 }
}

func TestOAuthRefreshCannotOverwriteConcurrentOwnerRotation(t *testing.T){
 entered:=make(chan struct{})
 releaseResponse:=make(chan struct{})
 providerServer:=httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  close(entered)
  <-releaseResponse
  w.Header().Set("Content-Type","application/json")
  _,_=w.Write([]byte(`{"access_token":"stale-access","refresh_token":"stale-refresh","token_type":"Bearer"}`))
 }))
 defer providerServer.Close()
 vault,err:=OpenVault(filepath.Join(t.TempDir(),"vault.json"),bytes.Repeat([]byte("z"),32))
 if err!=nil{t.Fatal(err)}
 handle,err:=vault.Store("vendor",[]byte(`{"access_token":"old-access","refresh_token":"old-refresh","token_type":"Bearer"}`))
 if err!=nil{t.Fatal(err)}
 done:=make(chan error,1)
 provider:=Provider{Name:"vendor",ClientID:"public-client",TokenURL:providerServer.URL}
 go func(){done<-RefreshOAuthCredential(context.Background(),provider,handle.ID,providerServer.Client(),vault)}()
 select{case <-entered:case <-time.After(5*time.Second):t.Fatal("provider never received refresh")}
 if _,err:=vault.Rotate(handle.ID,"vendor",[]byte(`{"access_token":"owner-access","refresh_token":"owner-refresh","token_type":"Bearer"}`));err!=nil{t.Fatal(err)}
 close(releaseResponse)
 if err:=<-done;err==nil{t.Fatal("stale provider response overwrote owner rotation")}
 if err:=vault.WithCredential(handle.ID,"vendor",func(data []byte)error{
  if !strings.Contains(string(data),"owner-access")||strings.Contains(string(data),"stale-access"){
   t.Fatal("concurrent refresh overwrote owner credential")
  }
  return nil
 });err!=nil{t.Fatal(err)}
}

func TestOAuthRefreshPreservesRefreshGrantWhenProviderOmitsReplacement(t *testing.T){
 providerServer:=httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  w.Header().Set("Content-Type","application/json")
  _,_=w.Write([]byte(`{"access_token":"replacement-access","token_type":"Bearer"}`))
 }))
 defer providerServer.Close()
 vault,err:=OpenVault(filepath.Join(t.TempDir(),"vault.json"),bytes.Repeat([]byte("q"),32));if err!=nil{t.Fatal(err)}
 handle,err:=vault.Store("vendor",[]byte(`{"access_token":"original","refresh_token":"kept-refresh","token_type":"Bearer"}`))
 if err!=nil{t.Fatal(err)}
 provider:=Provider{Name:"vendor",ClientID:"public-client",TokenURL:providerServer.URL}
 if err:=RefreshOAuthCredential(context.Background(),provider,handle.ID,providerServer.Client(),vault);err!=nil{t.Fatal(err)}
 if err:=vault.WithCredential(handle.ID,"vendor",func(data []byte)error{
  var fields map[string]string
  if err:=json.Unmarshal(data,&fields);err!=nil{t.Fatal(err)}
  if fields["access_token"]!="replacement-access"||fields["refresh_token"]!="kept-refresh"{t.Fatal("RFC 6749 refresh grant lost")}
  return nil
 });err!=nil{t.Fatal(err)}
 provider.TokenURL="http://example.com/token"
 if err:=RefreshOAuthCredential(context.Background(),provider,handle.ID,providerServer.Client(),vault);err==nil{
  t.Fatal("unencrypted refresh endpoint permitted")
 }
}
