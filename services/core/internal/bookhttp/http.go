package bookhttp

import (
 "errors"
 "net/http"

 "github.com/dbontr/homelab-new/system/books"
 "github.com/dbontr/homelab-new/system/internal/httpjson"
 "github.com/dbontr/homelab-new/system/store"
)

// Handler owns the book discovery and request HTTP contracts. The caller
// is responsible for authentication before dispatching to this handler.
type Handler struct {
 db *store.Store
 catalog *books.Catalog
}

// Register binds the book routes to an authenticated router and the same
// durable store used by native operations and the unified MCP dispatcher.
func Register(mux *http.ServeMux,db *store.Store,catalog *books.Catalog) {
 if mux==nil||db==nil{panic("book routes require a router and a durable store")}
 h:=&Handler{db:db,catalog:catalog}
 mux.HandleFunc("GET /v1/books/search",h.serve)
 mux.HandleFunc("GET /v1/books/lending",h.serve)
 mux.HandleFunc("GET /v1/books/requests",h.serve)
 mux.HandleFunc("POST /v1/books/requests",h.serve)
 mux.HandleFunc("PUT /v1/books/requests/{id}",h.serve)
}

func (h *Handler) serve(w http.ResponseWriter,r *http.Request){
 switch r.URL.Path {
 case "/v1/books/search":
  if h.catalog==nil{
   httpjson.Error(w,503,"book discovery has not been configured");return
  }
  query:=r.URL.Query().Get("q")
  result,err:=h.catalog.Search(r.Context(),query)
  if err!=nil{httpjson.Error(w,502,"book catalog is unavailable or the search is invalid");return}
  httpjson.Write(w,200,result)
 case "/v1/books/lending":
  if h.catalog==nil{httpjson.Error(w,503,"book lending discovery is unavailable");return}
  result,err:=h.catalog.FindReading(r.Context(),r.URL.Query().Get("isbn"))
  if err!=nil{httpjson.Error(w,400,"book lending lookup unavailable or ISBN invalid");return}
  httpjson.Write(w,200,result)
 case "/v1/books/requests":
  if r.Method==http.MethodGet{
   httpjson.Write(w,200,h.db.ListBookRequests(r.URL.Query().Get("status")))
   return
  }
  var input store.BookRequestInput
  if err:=httpjson.Decode(r,&input);err!=nil{httpjson.Error(w,400,"invalid book request");return}
  record,err:=h.db.AddBookRequest(input)
  switch{
  case errors.Is(err,store.ErrBookRequestConflict):
   httpjson.Error(w,409,"a matching book request already exists")
  case err!=nil:httpjson.Error(w,400,err.Error())
  default:httpjson.Write(w,201,record)
  }
 default:
  if r.Method!=http.MethodPut{
   httpjson.Error(w,405,"unsupported book request operation");return
  }
  var input struct{
   Status string `json:"status"`
   ExpectedRevision int `json:"expected_revision"`
  }
  if err:=httpjson.Decode(r,&input);err!=nil{httpjson.Error(w,400,"invalid book status update");return}
  record,err:=h.db.SetBookRequestStatus(r.PathValue("id"),input.Status,input.ExpectedRevision)
  switch{
  case errors.Is(err,store.ErrBookRequestNotFound):httpjson.Error(w,404,"book request not found")
  case errors.Is(err,store.ErrBookRequestConflict):httpjson.Error(w,409,"book request revision mismatch")
  case err!=nil:httpjson.Error(w,400,err.Error())
  default:httpjson.Write(w,200,record)
  }
 }
}

