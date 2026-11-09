package books

import (
 "context"
 "encoding/json"
 "errors"
 "io"
 "net/http"
 "net/url"
 "regexp"
 "strings"
 "time"
)

var isbnPattern=regexp.MustCompile(`^(?:[0-9]{13}|[0-9]{9}[0-9Xx])$`)
var editionIDPattern=regexp.MustCompile(`^OL[0-9]+M$`)
var workIDPart=regexp.MustCompile(`^OL[0-9]+W$`)

// LendingOption is a provider-owned, current lookup. The provider decides
// whether the patron can read or borrow; Eminence never records that claim.
type LendingOption struct {
 Provider string `json:"provider"`
 Match string `json:"match"`
 Status string `json:"status"`
 EditionID string `json:"edition_id,omitempty"`
 WorkID string `json:"work_id,omitempty"`
 ProviderURL string `json:"provider_url,omitempty"`
}
type LendingResult struct {
 ISBN string `json:"isbn"`
 Source string `json:"source"`
 Options []LendingOption `json:"options"`
 Note string `json:"note"`
}

func safeLendingURL(value string)string{
 parsed,err:=url.Parse(value)
 if err!=nil||parsed.Scheme!="https"||parsed.User!=nil||parsed.Fragment!=""||
  parsed.Opaque!=""||parsed.Host==""{return ""}
 switch strings.ToLower(parsed.Hostname()){
 case "openlibrary.org","www.openlibrary.org","archive.org","www.archive.org":
 default:return ""
 }
 if parsed.Port()!=""||!strings.HasPrefix(parsed.Path,"/"){return ""}
 return parsed.String()
}

func decodeLendingResult(isbn string,data []byte)(LendingResult,error){
 result:=LendingResult{
  ISBN:isbn,Source:"Open Library Read API",
  Options:[]LendingOption{},
  Note:"Availability is reported by Open Library at query time. Lending, loans, waiting lists, licensing and user authentication are controlled by the provider. Eminence never borrows or downloads books.",
 }
 var raw struct {
  Items []struct{
   Match string `json:"match"`
   Status string `json:"status"`
   ItemURL string `json:"itemURL"`
   EditionID string `json:"ol-edition-id"`
   WorkID string `json:"ol-work-id"`
  } `json:"items"`
 }
 if err:=json.Unmarshal(data,&raw);err!=nil{return LendingResult{},errors.New("invalid Open Library lending response")}
 if len(raw.Items)>100{return LendingResult{},errors.New("Open Library returned excessive lending matches")}
 for _,item:=range raw.Items{
  if len(result.Options)>=8{break}
  if item.Match!="exact"&&item.Match!="similar"{continue}
  switch item.Status{
  case "full access","lendable","checked out","restricted":
  default:continue
  }
  option:=LendingOption{Provider:"Open Library",Match:item.Match,Status:item.Status}
  if editionIDPattern.MatchString(item.EditionID){option.EditionID=item.EditionID}
  if workIDPart.MatchString(item.WorkID){option.WorkID=item.WorkID}
  if item.Status=="full access"||item.Status=="lendable"{
   option.ProviderURL=safeLendingURL(item.ItemURL)
  }
  result.Options=append(result.Options,option)
 }
 return result,nil
}

// FindReading is an owner-initiated, rate-limited, read-only availability
// inquiry for an exact book edition identifier.
func(c *Catalog)FindReading(ctx context.Context,isbn string)(LendingResult,error){
 if c==nil||!isbnPattern.MatchString(isbn){return LendingResult{},errors.New("ISBN-10 or ISBN-13 required")}
 c.mu.Lock()
 defer c.mu.Unlock()
 if elapsed:=time.Since(c.lastCall);elapsed<time.Second{
  timer:=time.NewTimer(time.Second-elapsed)
  defer timer.Stop()
  select{case <-timer.C:case <-ctx.Done():return LendingResult{},ctx.Err()}
 }
 c.lastCall=time.Now()
 req,err:=http.NewRequestWithContext(ctx,http.MethodGet,
  "https://openlibrary.org/api/volumes/brief/isbn/"+isbn+".json",nil)
 if err!=nil{return LendingResult{},errors.New("invalid book identifier")}
 req.Header.Set("Accept","application/json")
 req.Header.Set("User-Agent","HomelabBooks/1.0 ("+c.contact+")")
 resp,err:=c.client.Do(req)
 if err!=nil{return LendingResult{},errors.New("Open Library lending lookup unavailable")}
 defer resp.Body.Close()
 if resp.StatusCode!=200{return LendingResult{},errors.New("Open Library lending lookup unsuccessful")}
 if resp.ContentLength>512<<10{return LendingResult{},errors.New("Open Library lending response exceeds size limit")}
 body,err:=io.ReadAll(io.LimitReader(resp.Body,(512<<10)+1))
 if err!=nil||len(body)>512<<10{return LendingResult{},errors.New("invalid or excessive Open Library lending data")}
 return decodeLendingResult(isbn,body)
}
