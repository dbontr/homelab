package books

import(
 "context"
 "encoding/json"
 "io"
 "net/http"
 "strings"
 "testing"
 "time"
)

type lendingTransport func(*http.Request)(*http.Response,error)
func(f lendingTransport)RoundTrip(request *http.Request)(*http.Response,error){return f(request)}

func TestLendingFiltersRemoteLinksAndDoesNotClaimAcquisition(t *testing.T){
 for _,url:=range []string{
  "javascript:alert(1)",
  "http://openlibrary.org/borrow",
  "https://openlibrary.org.evil.example/borrow",
  "https://user:secret@openlibrary.org/borrow",
  "https://www.archive.org:444/private",
 }{
  if got:=safeLendingURL(url);got!=""{t.Fatalf("unsafe provider link accepted: %s",got)}
 }
 result,err:=decodeLendingResult("9780140328721",[]byte(`{"items":[
  {"match":"exact","status":"lendable","itemURL":"https://openlibrary.org/books/OL1234M/borrow","ol-edition-id":"OL1234M","ol-work-id":"OL88W"},
  {"match":"similar","status":"full access","itemURL":"https://www.archive.org/stream/edition"},
  {"match":"exact","status":"checked out","itemURL":"https://openlibrary.org/books/OL1234M/borrow"},
  {"match":"exact","status":"restricted","itemURL":"https://openlibrary.org/books/OL1234M/borrow"},
  {"match":"exact","status":"lendable","itemURL":"https://somewhere-else.example/redirect"},
  {"match":"unknown","status":"lendable","itemURL":"https://openlibrary.org/books/OL1234M/borrow"}
 ]}`))
 if err!=nil{t.Fatal(err)}
 if len(result.Options)!=5{t.Fatalf("unexpected candidate filtering: %+v",result)}
 if result.Options[0].ProviderURL!="https://openlibrary.org/books/OL1234M/borrow"||
  result.Options[0].EditionID!="OL1234M"||result.Options[0].WorkID!="OL88W"{
  t.Fatalf("provider-approved link was discarded: %+v",result.Options[0])
 }
 if result.Options[2].ProviderURL!=""||result.Options[3].ProviderURL!=""||
  result.Options[4].ProviderURL!=""{
  t.Fatalf("restricted or external links were exposed: %+v",result.Options)
 }
 if strings.Contains(strings.ToLower(result.Note),"successfully borrowed"){
  t.Fatal("availability was mistaken for a completed loan")
 }
 if _,err:=decodeLendingResult("9780140328721",[]byte("bad"));err==nil{
  t.Fatal("malformed upstream response was accepted")
 }
}

func TestLendingFetchUsesFixedProviderURLAndRejectsISBNInjection(t *testing.T){
 catalog,err:=New("books@example.org")
 if err!=nil{t.Fatal(err)}
 hits:=0
 catalog.client=&http.Client{
  Transport:lendingTransport(func(r *http.Request)(*http.Response,error){
   hits++
   if r.Method!="GET"||r.URL.Scheme!="https"||r.URL.Host!="openlibrary.org"||
    r.URL.Path!="/api/volumes/brief/isbn/9780140328721.json"||
    !strings.Contains(r.Header.Get("User-Agent"),"books@example.org"){
    t.Fatalf("invalid provider request: %s",r.URL.String())
   }
   body:=`{"items":[{"match":"exact","status":"lendable","itemURL":"https://openlibrary.org/books/OL1M/borrow"}]}`
   return &http.Response{StatusCode:200,Header:http.Header{"Content-Type":[]string{"application/json"}},Body:io.NopCloser(strings.NewReader(body))},nil
  }),
  Timeout:3*time.Second,
  CheckRedirect:func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse},
 }
 result,err:=catalog.FindReading(context.Background(),"9780140328721")
 if err!=nil||len(result.Options)!=1||hits!=1{
  t.Fatalf("book lending lookup failed: %+v %v",result,err)
 }
 for _,input:=range []string{"9780140328721/../secret","http://other.example","9780140328721%2F","bad","978-0-14-032872-1"}{
  if _,err:=catalog.FindReading(context.Background(),input);err==nil{
   t.Fatalf("unsafe ISBN accepted: %q",input)
  }
 }
 if hits!=1{t.Fatalf("rejected ISBN caused a provider call: %d",hits)}
}

func TestLendingWireResultIsLimited(t *testing.T){
 records:=make([]map[string]any,101)
 for i:=range records{records[i]=map[string]any{
  "match":"exact","status":"lendable","itemURL":"https://openlibrary.org/borrow",
 }}
 body,_:=json.Marshal(map[string]any{"items":records})
 if _,err:=decodeLendingResult("9780140328721",body);err==nil{
  t.Fatal("excessive online lending candidates accepted")
 }
}
