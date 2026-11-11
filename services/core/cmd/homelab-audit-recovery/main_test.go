package main

import (
 "encoding/json"
 "strings"
 "testing"
 "time"
)

func TestReconciliationRequiresExactResolvedFinding(t *testing.T){
 start:=time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
 note:=evidence{
  AttemptID:strings.Repeat("a",32),
  Operation:"infrastructure__service_restart",
  Target:"saturn",ArgumentsSHA256:strings.Repeat("b",64),
  Finding:"not_applied",VerifiedBy:"owner terminal",
  InspectedAt:start.Add(10*time.Minute).Format(time.RFC3339),
  Explanation:"Independently inspected the system service and verified no restart took place.",
 }
 encoded,err:=json.Marshal(note)
 if err!=nil{t.Fatal(err)}
 expected,err:=verifyEvidence(encoded,note.AttemptID,note.Operation,note.Target,
  note.ArgumentsSHA256,start)
 if err!=nil||len(expected)!=64{
  t.Fatalf("valid evidence rejected: %s, %v",expected,err)
 }
 for _,tc:=range []struct{
  name string
  modify func(*evidence)
 }{
  {"wrong attempt",func(n *evidence){n.AttemptID=strings.Repeat("c",32)}},
  {"wrong operation",func(n *evidence){n.Operation="infrastructure__service_stop"}},
  {"wrong target",func(n *evidence){n.Target="venus"}},
  {"wrong arguments",func(n *evidence){n.ArgumentsSHA256=strings.Repeat("d",64)}},
  {"unknown finding",func(n *evidence){n.Finding="unknown"}},
  {"short explanation",func(n *evidence){n.Explanation="Looks fine"}},
  {"missing verifier",func(n *evidence){n.VerifiedBy=""}},
  {"inspection before attempt",func(n *evidence){
   n.InspectedAt=start.Add(-time.Minute).Format(time.RFC3339)
  }},
  {"inspection after current time",func(n *evidence){
   n.InspectedAt=time.Now().Add(time.Hour).Format(time.RFC3339)
  }},
 }{
  t.Run(tc.name,func(t *testing.T){
   mutated:=note
   tc.modify(&mutated)
   raw,err:=json.Marshal(mutated)
   if err!=nil{t.Fatal(err)}
   if _,err:=verifyEvidence(raw,note.AttemptID,note.Operation,note.Target,
    note.ArgumentsSHA256,start);err==nil{
    t.Fatal("invalid operator evidence was accepted")
   }
  })
 }
}

func TestReconciliationRejectsAmbiguousJSONAndAdditionalFields(t *testing.T){
 start:=time.Now().UTC().Add(-time.Hour)
 record:=evidence{
  AttemptID:strings.Repeat("a",32),Operation:"infrastructure__service_restart",
  Target:"saturn",ArgumentsSHA256:strings.Repeat("b",64),
  Finding:"applied",VerifiedBy:"owner console",
  InspectedAt:start.Add(time.Minute).Format(time.RFC3339),
  Explanation:"Verified the requested operation in the independent system journal.",
 }
 raw,err:=json.Marshal(record)
 if err!=nil{t.Fatal(err)}
 duplicate:=strings.Replace(string(raw),`"finding":"applied"`,
  `"finding":"applied","finding":"not_applied"`,1)
 if _,err:=verifyEvidence([]byte(duplicate),record.AttemptID,record.Operation,
  record.Target,record.ArgumentsSHA256,start);err==nil{
  t.Fatal("duplicate JSON keys accepted")
 }
 unexpected:=strings.Replace(string(raw),`"finding":"applied"`,
  `"finding":"applied","override":true`,1)
 if _,err:=verifyEvidence([]byte(unexpected),record.AttemptID,record.Operation,
  record.Target,record.ArgumentsSHA256,start);err==nil{
  t.Fatal("unknown JSON evidence key accepted")
 }
}
