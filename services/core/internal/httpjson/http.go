// Package httpjson defines the private service's stable JSON envelope and
// bounded request decoder. Outer transports retain responsibility for auth.
package httpjson

import (
 "encoding/json"
 "errors"
 "io"
 "net/http"
)

const maxRequestBody=1<<20

func Write(w http.ResponseWriter,status int,body any){
 w.Header().Set("Content-Type","application/json")
 w.WriteHeader(status)
 _=json.NewEncoder(w).Encode(body)
}

func Error(w http.ResponseWriter,status int,message string){
 Write(w,status,map[string]string{"error":message})
}

func Decode(r *http.Request,target any)error{
 r.Body=http.MaxBytesReader(nil,r.Body,maxRequestBody)
 decoder:=json.NewDecoder(r.Body)
 decoder.DisallowUnknownFields()
 if err:=decoder.Decode(target);err!=nil{return err}
 var extra any
 if err:=decoder.Decode(&extra);err!=io.EOF{
  return errors.New("request must contain one JSON object")
 }
 return nil
}
