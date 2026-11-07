package health

import (
 "context"
 "encoding/json"
 "errors"
 "testing"
 "time"
)

func TestJournalVersionsAndSource(t *testing.T){
 s:=testStore(t)
 ctx:=context.Background()
 initial,err:=s.SaveJournal(ctx,JournalEntry{
  Category:"medication", Title:"Daily record",OccurredAt:time.Now(),
  Source:"manual",Attributes:json.RawMessage("{\"dose\":5,\"unit\":\"mg\"}"),
 },0)
 if err!=nil{t.Fatal(err)}
 if initial.Revision!=1||initial.ID==""{t.Fatalf("invalid initial journal entry: %+v",initial)}
 initial.Title="Daily record confirmed"
 revised,err:=s.SaveJournal(ctx,initial,1)
 if err!=nil{t.Fatal(err)}
 if revised.Revision!=2{t.Fatal("second revision must be 2")}
 if _,err=s.SaveJournal(ctx,initial,1);!errors.Is(err,ErrConflict){t.Fatalf("stale journal edit accepted: %v",err)}
 actual,err:=s.GetJournal(ctx,revised.ID)
 if err!=nil||actual.Title!=revised.Title{t.Fatalf("read: %+v %v",actual,err)}
 history,err:=s.JournalHistory(ctx,revised.ID)
 if err!=nil||len(history)!=2||history[0].Revision!=1||history[1].Revision!=2{
  t.Fatalf("journal revision history: %+v %v",history,err)
 }
 list,err:=s.ListJournal(ctx,"medication",10)
 if err!=nil||len(list)!=1{t.Fatalf("journal list: %+v %v",list,err)}
}

func TestJournalRejectsMalformedAndChangedCategory(t *testing.T){
 s:=testStore(t);ctx:=context.Background()
 invalid:=JournalEntry{Category:"laboratory",Title:"Lab",OccurredAt:time.Now(),
  Source:"manual",Attributes:json.RawMessage("null")}
 if _,err:=s.SaveJournal(ctx,invalid,0);err==nil{t.Fatal("accepted null attributes")}
 invalid.Attributes=json.RawMessage("{\"value\":3.5,\"unit\":\"mg/L\"}")
 saved,err:=s.SaveJournal(ctx,invalid,0);if err!=nil{t.Fatal(err)}
 saved.Category="medication"
 if _,err:=s.SaveJournal(ctx,saved,saved.Revision);err==nil{t.Fatal("changed journal category")}
}
