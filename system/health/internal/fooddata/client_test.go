package fooddata
import(
 "context"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
)
func TestBarcodeReadOnly(t *testing.T){
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.Method!=http.MethodGet{t.Errorf("unexpected method %s",r.Method)}
  if r.URL.Path!="/api/v3/product/3017620422003"{t.Errorf("bad path %s",r.URL.Path)}
  if !strings.HasPrefix(r.Header.Get("User-Agent"),"personal-health-mcp/"){t.Error("missing user agent")}
  _,_=w.Write([]byte(`{"status":"success","product":{"product_name":"Example","nutriments":{"proteins_100g":13}}}`))
 }))
 defer server.Close()
 c:=New(Config{HTTP:server.Client(),OpenFoodFactsBase:server.URL})
 result,err:=c.Barcode(context.Background(),"3017620422003")
 if err!=nil||!strings.Contains(string(result),"Example"){t.Fatalf("barcode: %s %v",result,err)}
 if _,err:=c.Barcode(context.Background(),"http://example.com");err==nil{t.Fatal("unsafe barcode accepted")}
}
func TestUSDAKeyRequired(t *testing.T){
 c:=New(Config{})
 if _,err:=c.USDA(context.Background(),"oats",10);err==nil{t.Fatal("missing key accepted")}
}
