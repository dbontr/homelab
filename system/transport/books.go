package transport

import (
 "context"
 "encoding/json"
 "errors"
 "net/http"

 "github.com/dbontr/homelab-new/system/store"
)

func (s *Server) bookRoutes(w http.ResponseWriter,r *http.Request){
 switch r.URL.Path {
 case "/v1/books/search":
  if s.bookCatalog==nil{
   writeError(w,503,"book discovery has not been configured");return
  }
  query:=r.URL.Query().Get("q")
  result,err:=s.bookCatalog.Search(r.Context(),query)
  if err!=nil{writeError(w,502,"book catalog is unavailable or the search is invalid");return}
  writeJSON(w,200,result)
 case "/v1/books/lending":
  if s.bookCatalog==nil{writeError(w,503,"book lending discovery is unavailable");return}
  result,err:=s.bookCatalog.FindReading(r.Context(),r.URL.Query().Get("isbn"))
  if err!=nil{writeError(w,400,"book lending lookup unavailable or ISBN invalid");return}
  writeJSON(w,200,result)
 case "/v1/books/requests":
  if r.Method==http.MethodGet{
   writeJSON(w,200,s.db.ListBookRequests(r.URL.Query().Get("status")))
   return
  }
  var input store.BookRequestInput
  if err:=decode(r,&input);err!=nil{writeError(w,400,"invalid book request");return}
  record,err:=s.db.AddBookRequest(input)
  switch{
  case errors.Is(err,store.ErrBookRequestConflict):
   writeError(w,409,"a matching book request already exists")
  case err!=nil:writeError(w,400,err.Error())
  default:writeJSON(w,201,record)
  }
 default:
  if r.Method!=http.MethodPut{
   writeError(w,405,"unsupported book request operation");return
  }
  var input struct{
   Status string `json:"status"`
   ExpectedRevision int `json:"expected_revision"`
  }
  if err:=decode(r,&input);err!=nil{writeError(w,400,"invalid book status update");return}
  record,err:=s.db.SetBookRequestStatus(r.PathValue("id"),input.Status,input.ExpectedRevision)
  switch{
  case errors.Is(err,store.ErrBookRequestNotFound):writeError(w,404,"book request not found")
  case errors.Is(err,store.ErrBookRequestConflict):writeError(w,409,"book request revision mismatch")
  case err!=nil:writeError(w,400,err.Error())
  default:writeJSON(w,200,record)
  }
 }
}

func (s *Server) bookTool(ctx context.Context,name string,raw json.RawMessage)(any,error){
 var args struct {
  Query string `json:"query"`
  Status string `json:"status"`
  ID string `json:"id"`
  ExpectedRevision int `json:"expected_revision"`
  Request store.BookRequestInput `json:"request"`
 }
 if len(raw)>0&&json.Unmarshal(raw,&args)!=nil{return nil,errors.New("invalid book arguments")}
 switch name{
 case "book_search":
  if s.bookCatalog==nil{return nil,errors.New("book discovery is not configured")}
  return s.bookCatalog.Search(ctx,args.Query)
 case "book_lending":
  if s.bookCatalog==nil{return nil,errors.New("book lending discovery is not configured")}
  return s.bookCatalog.FindReading(ctx,args.Query)
 case "book_request_list":
  return s.db.ListBookRequests(args.Status),nil
 case "book_request_add":
  return s.db.AddBookRequest(args.Request)
 case "book_request_status":
  return s.db.SetBookRequestStatus(args.ID,args.Status,args.ExpectedRevision)
 default:
  return nil,errors.New("unknown book operation")
 }
}
