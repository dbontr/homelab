// Package books supports on-demand catalog discovery without acquisition.
package books

import (
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net/http"
 "net/url"
 "regexp"
 "strings"
 "sync"
 "time"
)

var workID=regexp.MustCompile(`^/works/OL[0-9]+W$`)
var contactEmail=regexp.MustCompile(`^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$`)

type Work struct {
 WorkID string `json:"work_id"`
 Title string `json:"title"`
 Authors []string `json:"authors"`
 FirstPublished int `json:"first_published,omitempty"`
 EditionCount int `json:"edition_count,omitempty"`
 CatalogURL string `json:"catalog_url"`
}

type SearchResult struct{
 Query string `json:"query"`
 Source string `json:"source"`
 Works []Work `json:"works"`
}

type cacheEntry struct {
 result SearchResult
 expiry time.Time
}

// Catalog uses the official Open Library on-demand search endpoint. It sends
// at most one request per second, limits each response and caches searches.
type Catalog struct {
 contact string
 client *http.Client
 mu sync.Mutex
 cache map[string]cacheEntry
 lastCall time.Time
}

func New(contact string)(*Catalog,error){
 contact=strings.TrimSpace(contact)
 if len(contact)>160||!contactEmail.MatchString(contact){
  return nil,errors.New("Open Library contact email is required for catalog access")
 }
 return &Catalog{
  contact:contact,cache:map[string]cacheEntry{},
  client:&http.Client{
   Timeout:9*time.Second,
   Transport:&http.Transport{MaxIdleConnsPerHost:2,Proxy:nil,ResponseHeaderTimeout:6*time.Second},
   CheckRedirect:func(*http.Request,[]*http.Request)error{return errors.New("catalog redirects are disabled")},
  },
 },nil
}

func validQuery(q string)bool{
 if len(q)<2||len(q)>160{return false}
 if strings.TrimSpace(q)!=q{return false}
 return !strings.ContainsAny(q,"\r\n\x00")
}

func(c *Catalog) Search(ctx context.Context,q string)(SearchResult,error){
 q=strings.TrimSpace(q)
 if !validQuery(q){return SearchResult{},errors.New("search must contain 2 to 160 characters")}
 key:=strings.ToLower(q)
 c.mu.Lock()
 defer c.mu.Unlock()
 now:=time.Now()
 if entry,ok:=c.cache[key];ok&&entry.expiry.After(now){return entry.result,nil}
 if elapsed:=time.Since(c.lastCall);elapsed<time.Second{
  timer:=time.NewTimer(time.Second-elapsed)
  defer timer.Stop()
  select{case <-timer.C:case <-ctx.Done():return SearchResult{},ctx.Err()}
 }
 c.lastCall=time.Now()
 values:=url.Values{
  "q":{q},
  "limit":{"12"},
  "fields":{"key,title,author_name,first_publish_year,edition_count"},
 }
 req,err:=http.NewRequestWithContext(ctx,http.MethodGet,
  "https://openlibrary.org/search.json?"+values.Encode(),nil)
 if err!=nil{return SearchResult{},err}
 req.Header.Set("Accept","application/json")
 req.Header.Set("User-Agent","HomelabBooks/1.0 ("+c.contact+")")
 response,err:=c.client.Do(req)
 if err!=nil{return SearchResult{},errors.New("book catalog unavailable")}
 defer response.Body.Close()
 if response.StatusCode!=http.StatusOK{return SearchResult{},fmt.Errorf("book catalog returned HTTP %d",response.StatusCode)}
 if response.ContentLength>1<<20{return SearchResult{},errors.New("catalog response exceeds size limit")}
 data,err:=io.ReadAll(io.LimitReader(response.Body,(1<<20)+1))
 if err!=nil{return SearchResult{},err}
 if len(data)>1<<20{return SearchResult{},errors.New("catalog response exceeds size limit")}
 var decoded struct{
  Docs []struct{
   Key string `json:"key"`
   Title string `json:"title"`
   AuthorName []string `json:"author_name"`
   FirstPublishYear int `json:"first_publish_year"`
   EditionCount int `json:"edition_count"`
  } `json:"docs"`
 }
 if err:=json.Unmarshal(data,&decoded);err!=nil{return SearchResult{},errors.New("invalid book catalog response")}
 result:=SearchResult{Query:q,Source:"Open Library",Works:[]Work{}}
 for _,item:=range decoded.Docs{
  if !workID.MatchString(item.Key)||strings.TrimSpace(item.Title)==""{continue}
  if len(result.Works)>=12{break}
  authors:=item.AuthorName
  if len(authors)>12{authors=authors[:12]}
  year:=item.FirstPublishYear
  if year<0||year>3000{year=0}
  editions:=item.EditionCount
  if editions<0{editions=0}
  result.Works=append(result.Works,Work{
   WorkID:item.Key,Title:item.Title,Authors:authors,
   FirstPublished:year,EditionCount:editions,
   CatalogURL:"https://openlibrary.org"+item.Key,
  })
 }
 if len(c.cache)>=128{clear(c.cache)}
 c.cache[key]=cacheEntry{result:result,expiry:time.Now().Add(15*time.Minute)}
 return result,nil
}
