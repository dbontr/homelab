package calendar

import (
 "context"
 "errors"
 "fmt"
 "io"
 "net/http"
 "net/url"
 "strings"
)
type Event struct {
 UID string `json:"uid,omitempty"`
 Summary string `json:"summary,omitempty"`
 Start string `json:"start,omitempty"`
 End string `json:"end,omitempty"`
 Location string `json:"location,omitempty"`
 Recurrence string `json:"recurrence,omitempty"`
 TimeZone string `json:"time_zone,omitempty"`
}
type Feed struct {
 Events []Event `json:"events"`
 RecurrencesUnexpanded bool `json:"recurrences_unexpanded"`
 Note string `json:"note"`
}
func ValidateURL(raw string)error{
 u,e:=url.Parse(raw);if e!=nil{return errors.New("invalid calendar URL")}
 if u.Scheme!="https"||u.Host==""||u.User!=nil||u.Fragment!=""{
  return errors.New("calendar URL must be HTTPS and must not embed credentials")
 }
 return nil
}
func Fetch(ctx context.Context,client *http.Client,rawURL string)(Feed,error){
 if e:=ValidateURL(rawURL);e!=nil{return Feed{},e}
 req,e:=http.NewRequestWithContext(ctx,http.MethodGet,rawURL,nil);if e!=nil{return Feed{},errors.New("invalid calendar request")}
 req.Header.Set("Accept","text/calendar")
 response,e:=client.Do(req);if e!=nil{return Feed{},errors.New("calendar feed request failed")}
 defer response.Body.Close()
 if response.StatusCode!=200{return Feed{},fmt.Errorf("calendar feed returned HTTP %d",response.StatusCode)}
 content,e:=io.ReadAll(io.LimitReader(response.Body,(8<<20)+1));if e!=nil{return Feed{},e}
 if len(content)>(8<<20){return Feed{},errors.New("calendar feed exceeds 8 MiB")}
 return Parse(string(content))
}
func field(line string)(name,attrs,value string){
 parts:=strings.SplitN(line,":",2);if len(parts)!=2{return "","",""}
 key:=strings.SplitN(parts[0],";",2);if len(key)==2{attrs=key[1]}
 return strings.ToUpper(key[0]),attrs,parts[1]
}
func timeZone(attrs string)string{
 for _,part:=range strings.Split(attrs,";"){
  if strings.HasPrefix(strings.ToUpper(part),"TZID="){return strings.Trim(part[6:],"\"")}
 }
 return ""
}
func decodeText(raw string)string{
 raw=strings.ReplaceAll(raw,"\\n","\n")
 raw=strings.ReplaceAll(raw,"\\N","\n")
 raw=strings.ReplaceAll(raw,"\\,",",")
 raw=strings.ReplaceAll(raw,"\\;",";")
 return strings.ReplaceAll(raw,"\\\\","\\")
}
func Parse(raw string)(Feed,error){
 if !strings.Contains(raw,"BEGIN:VCALENDAR"){return Feed{},errors.New("not an iCalendar feed")}
 lines:=strings.Split(strings.ReplaceAll(raw,"\r\n","\n"),"\n")
 unfolded:=make([]string,0,len(lines))
 for _,line:=range lines{
  if len(line)>0&&(line[0]==' '||line[0]=='\t')&&len(unfolded)>0{unfolded[len(unfolded)-1]+=line[1:]}else{unfolded=append(unfolded,line)}
 }
 feed:=Feed{Events:[]Event{},Note:"Event definitions only; recurrence rules have not been expanded into individual occurrences."}
 inEvent:=false;var current Event
 for _,line:=range unfolded{
  switch strings.TrimSpace(line){
  case "BEGIN:VEVENT":inEvent=true;current=Event{};continue
  case "END:VEVENT":if inEvent{feed.Events=append(feed.Events,current)};inEvent=false;continue
  }
  if !inEvent{continue}
  key,attrs,value:=field(line)
  switch key{
  case "UID":current.UID=value
  case "SUMMARY":current.Summary=decodeText(value)
  case "DTSTART":current.Start=value;current.TimeZone=timeZone(attrs)
  case "DTEND":current.End=value
  case "LOCATION":current.Location=decodeText(value)
  case "RRULE":current.Recurrence=value;feed.RecurrencesUnexpanded=true
  }
 }
 return feed,nil
}
