package store

import (
 "errors"
 "regexp"
 "sort"
 "strings"
 "time"
)

var bookWorkID=regexp.MustCompile(`^/works/OL[0-9]+W$`)

type BookRequest struct {
 ID string `json:"id"`
 WorkID string `json:"work_id"`
 Title string `json:"title"`
 Authors []string `json:"authors,omitempty"`
 Format string `json:"format"`
 Status string `json:"status"`
 Revision int `json:"revision"`
 CreatedAt time.Time `json:"created_at"`
 UpdatedAt time.Time `json:"updated_at"`
}

type BookRequestInput struct {
 WorkID string `json:"work_id"`
 Title string `json:"title"`
 Authors []string `json:"authors,omitempty"`
 Format string `json:"format"`
}

var ErrBookRequestConflict=errors.New("book request conflict")
var ErrBookRequestNotFound=errors.New("book request not found")

func normalizeBook(in BookRequestInput)(BookRequestInput,error){
 in.Title=strings.TrimSpace(in.Title)
 if !bookWorkID.MatchString(in.WorkID)||in.Title==""||len(in.Title)>300{
  return in,errors.New("valid Open Library work ID and title are required")
 }
 if in.Format==""{in.Format="ebook"}
 switch in.Format{
 case "ebook","audiobook","print":
 default:return in,errors.New("book format must be ebook, audiobook or print")
 }
 if len(in.Authors)>12{return in,errors.New("too many book authors")}
 for i:=range in.Authors{
  in.Authors[i]=strings.TrimSpace(in.Authors[i])
  if in.Authors[i]==""||len(in.Authors[i])>180{
   return in,errors.New("invalid book author")
  }
 }
 return in,nil
}

func (s *Store) AddBookRequest(input BookRequestInput)(BookRequest,error){
 input.Authors=append([]string(nil),input.Authors...)
 normalized,err:=normalizeBook(input)
 if err!=nil{return BookRequest{},err}
 s.mu.Lock()
 defer s.mu.Unlock()
 for _,previous:=range s.data.BookRequests{
  if previous.WorkID==normalized.WorkID&&previous.Format==normalized.Format&&previous.Status!="cancelled"{
   return BookRequest{},ErrBookRequestConflict
  }
 }
 key,err:=id()
 if err!=nil{return BookRequest{},err}
 now:=time.Now().UTC()
 request:=BookRequest{ID:key,WorkID:normalized.WorkID,Title:normalized.Title,
  Authors:normalized.Authors,Format:normalized.Format,Status:"requested",
  Revision:1,CreatedAt:now,UpdatedAt:now}
 next:=clone(s.data)
 next.BookRequests[key]=request
 if err:=s.save(next);err!=nil{return BookRequest{},err}
 request.Authors=append([]string(nil),request.Authors...)
 return request,nil
}

func (s *Store) ListBookRequests(status string)[]BookRequest{
 s.mu.Lock()
 defer s.mu.Unlock()
 result:=make([]BookRequest,0,len(s.data.BookRequests))
 for _,item:=range s.data.BookRequests{
  if status!=""&&item.Status!=status{continue}
  item.Authors=append([]string(nil),item.Authors...)
  result=append(result,item)
 }
 sort.Slice(result,func(i,j int)bool{
  if result[i].CreatedAt.Equal(result[j].CreatedAt){return result[i].ID<result[j].ID}
  return result[i].CreatedAt.After(result[j].CreatedAt)
 })
 return result
}

func (s *Store) SetBookRequestStatus(key,status string,revision int)(BookRequest,error){
 if status!="requested"&&status!="fulfilled"&&status!="cancelled"{
  return BookRequest{},errors.New("unsupported book request status")
 }
 s.mu.Lock()
 defer s.mu.Unlock()
 current,found:=s.data.BookRequests[key]
 if !found{return BookRequest{},ErrBookRequestNotFound}
 if revision!=current.Revision{return BookRequest{},ErrBookRequestConflict}
 if current.Status==status{
  current.Authors=append([]string(nil),current.Authors...)
  return current,nil
 }
 if current.Status!="requested"{
  return BookRequest{},errors.New("only pending book requests can be completed or cancelled")
 }
 next:=clone(s.data)
 current.Revision++
 current.Status=status
 current.UpdatedAt=time.Now().UTC()
 next.BookRequests[key]=current
 if err:=s.save(next);err!=nil{return BookRequest{},err}
 current.Authors=append([]string(nil),current.Authors...)
 return current,nil
}
