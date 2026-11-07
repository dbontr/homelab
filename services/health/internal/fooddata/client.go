// Package fooddata retrieves food facts from public nutrition data sources.
package fooddata

import (
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net/http"
 "net/url"
 "regexp"
 "strconv"
 "strings"
 "time"
)

const maxBytes=4<<20
var barcodePattern=regexp.MustCompile(`^[0-9]{8,14}$`)
type Config struct{
 USDAKey string
 HTTP *http.Client
 OpenFoodFactsBase string
 USDABase string
}
type Client struct{
 cfg Config
 http *http.Client
 offBase string
 usdaBase string
}
func New(cfg Config)*Client{
 client:=cfg.HTTP
 if client==nil{client=&http.Client{Timeout:20*time.Second,CheckRedirect:func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}}}
 off:="https://world.openfoodfacts.org"
 usda:="https://api.nal.usda.gov"
 if cfg.OpenFoodFactsBase!=""{off=strings.TrimRight(cfg.OpenFoodFactsBase,"/")}
 if cfg.USDABase!=""{usda=strings.TrimRight(cfg.USDABase,"/")}
 return &Client{cfg:cfg,http:client,offBase:off,usdaBase:usda}
}
func (c *Client)request(ctx context.Context,uri string)(json.RawMessage,error){
 req,err:=http.NewRequestWithContext(ctx,http.MethodGet,uri,nil)
 if err!=nil{return nil,err}
 req.Header.Set("Accept","application/json")
 req.Header.Set("User-Agent","personal-health-mcp/0.1 (https://github.com/dbontr/personal-health-mcp)")
 resp,err:=c.http.Do(req)
 if err!=nil{return nil,err}
 defer resp.Body.Close()
 if resp.StatusCode==http.StatusNotFound{return nil,errors.New("food item was not found")}
 if resp.StatusCode!=http.StatusOK{return nil,fmt.Errorf("food data provider returned HTTP %d",resp.StatusCode)}
 data,err:=io.ReadAll(io.LimitReader(resp.Body,maxBytes+1))
 if err!=nil{return nil,err}
 if len(data)>maxBytes{return nil,errors.New("food response exceeds size limit")}
 if !json.Valid(data){return nil,errors.New("food provider returned invalid JSON")}
 return data,nil
}
func (c *Client)Barcode(ctx context.Context,code string)(json.RawMessage,error){
 if !barcodePattern.MatchString(code){return nil,errors.New("barcode must be 8-14 digits")}
 data,err:=c.request(ctx,c.offBase+"/api/v3/product/"+code)
 if err!=nil{return nil,err}
 var response struct{
  Status string `json:"status"`
  Product json.RawMessage `json:"product"`
 }
 if err=json.Unmarshal(data,&response);err!=nil{return nil,err}
 if len(response.Product)==0||string(response.Product)=="null"{return nil,errors.New("no usable product information found")}
 return data,nil
}
func(c *Client)USDA(ctx context.Context,query string,limit int)(json.RawMessage,error){
 if c.cfg.USDAKey==""{return nil,errors.New("USDA_FDC_API_KEY is not configured")}
 if strings.TrimSpace(query)==""||len(query)>200{return nil,errors.New("food query is required and at most 200 characters")}
 if limit<1||limit>25{return nil,errors.New("limit must be 1 to 25")}
 u,err:=url.Parse(c.usdaBase+"/fdc/v1/foods/search");if err!=nil{return nil,err}
 q:=u.Query();q.Set("api_key",c.cfg.USDAKey);q.Set("query",query);q.Set("pageSize",strconv.Itoa(limit));u.RawQuery=q.Encode()
 return c.request(ctx,u.String())
}
func(c *Client)USDADetail(ctx context.Context,fdcID int64)(json.RawMessage,error){
 if c.cfg.USDAKey==""{return nil,errors.New("USDA_FDC_API_KEY is not configured")}
 if fdcID<=0{return nil,errors.New("fdc_id must be positive")}
 u,err:=url.Parse(c.usdaBase+"/fdc/v1/food/"+strconv.FormatInt(fdcID,10));if err!=nil{return nil,err}
 q:=u.Query();q.Set("api_key",c.cfg.USDAKey);u.RawQuery=q.Encode()
 return c.request(ctx,u.String())
}
