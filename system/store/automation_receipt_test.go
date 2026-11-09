package store

import (
 "path/filepath"
 "strings"
 "testing"
)

func TestTaskCreationReceiptPersistsAcrossRestarts(t *testing.T){
 path:=filepath.Join(t.TempDir(),"personal.json")
 s,err:=Open(path,"UTC")
 if err!=nil{t.Fatal(err)}
 key:=strings.Repeat("a",64)
 input:=TaskInput{Title:"Read a research paper",Links:[]string{"homelab:project:physics"}}
 first,err:=s.AddTaskOnce(key,input)
 if err!=nil{t.Fatal(err)}
 replay,err:=s.AddTaskOnce(key,input)
 if err!=nil||replay.ID!=first.ID{t.Fatalf("replay duplicated a task: %v %v",replay,err)}
 s,err=Open(path,"UTC")
 if err!=nil{t.Fatal(err)}
 again,err:=s.AddTaskOnce(key,input)
 if err!=nil||again.ID!=first.ID{t.Fatalf("receipt was not durable: %v %v",again,err)}
 input.Title="Read a different paper"
 if _,err:=s.AddTaskOnce(key,input);err==nil{t.Fatal("idempotency key accepted different payload")}
 if len(s.Tasks(""))!=1{t.Fatal("workflow replay created a duplicate")}
}
