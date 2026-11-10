package transport

import (
 "context"
 "encoding/json"
 "errors"

 "github.com/dbontr/homelab-new/system/store"
)

func (s *Server) bookTool(ctx context.Context,name string,raw json.RawMessage)(any,error){
 var args struct {
  Query string `json:"query"`
  ISBN string `json:"isbn"`
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
  return s.bookCatalog.FindReading(ctx,args.ISBN)
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
