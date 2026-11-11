package store

import (
 "path/filepath"
 "strings"
 "testing"
)

func TestContinuityCheckpointPersistsWithRevisionAndPortableHandoff(t *testing.T){
 file:=filepath.Join(t.TempDir(),"state.json")
 state,err:=Open(file,"UTC")
 if err!=nil{t.Fatal(err)}
 input:=ContinuityInput{
  Key:"research-session-1",ProjectID:"ising-machine",
  Objective:"Finish the physical Ising machine prototype",
  Summary:"The VHDL architecture is sketched, but the board wiring is unverified.",
  NextSteps:[]string{"Inspect board pinout","Commit testbench"},
  Evidence:[]string{"PR #150","design/ising-vhdl.vhd"},
  ExpectedRevision:0,
 }
 saved,pressure,err:=state.SaveContinuity(input)
 if err!=nil||saved.Revision!=1||pressure.Status!="unknown"||
  pressure.Source!="unavailable"{
  t.Fatalf("missing host tokens were misrepresented: %+v %+v %v",
   saved,pressure,err)
 }
 input.ExpectedRevision=1
 input.ReportedInputTokens=110_000
 input.ContextWindowTokens=128_000
 input.ReservedOutputTokens=4_000
 saved,pressure,err=state.SaveContinuity(input)
 if err!=nil||saved.Revision!=2||pressure.Status!="handoff_recommended"||
  pressure.Source!="caller_reported_unverified"{
  t.Fatalf("high usage did not recommend handoff: %+v %v",pressure,err)
 }
 if _,_,err:=state.SaveContinuity(input);err==nil{
  t.Fatal("stale checkpoint revision overwrote current state")
 }
 other,err:=Open(file,"UTC")
 if err!=nil{t.Fatal(err)}
 handoff,err:=other.ContinuityHandoff(input.Key)
 if err!=nil{t.Fatal(err)}
 note,ok:=handoff["handoff"].(string)
 if !ok||!strings.Contains(note,input.Objective)||
  !strings.Contains(note,input.Evidence[1])||
  !strings.Contains(note,"testbench"){
  t.Fatal("handoff did not preserve the project working context")
 }
 if handoff["automatic_chat_transfer"]!=false{
  t.Fatal("MCP falsely claimed automatic ChatGPT conversation transfer")
 }
}

func TestContinuityRejectsUnboundedOrInventedUsage(t *testing.T){
 state,err:=Open(filepath.Join(t.TempDir(),"state.json"),"UTC")
 if err!=nil{t.Fatal(err)}
 example:=ContinuityInput{Key:"session-1",Objective:"Design",Summary:"Current checkpoint",ExpectedRevision:0}
 for _,mutate:=range []func(*ContinuityInput){
  func(in *ContinuityInput){in.Key="../other"},
  func(in *ContinuityInput){in.Objective=strings.Repeat("x",1501)},
  func(in *ContinuityInput){in.Summary="contains\x00control"},
  func(in *ContinuityInput){in.ReportedInputTokens=100},
  func(in *ContinuityInput){in.ContextWindowTokens=8192;in.ReportedInputTokens=9000},
  func(in *ContinuityInput){in.NextSteps=[]string{strings.Repeat("x",801)}},
 }{
  invalid:=example
  mutate(&invalid)
  if _,_,err:=state.SaveContinuity(invalid);err==nil{
   t.Fatal("invalid continuity checkpoint accepted",invalid.Key)
  }
 }
 if _,_,err:=state.Continuity("session-1");err==nil{
  t.Fatal("failed checkpoint created persistent state")
 }
}

func TestContextPressureThresholdsAreReportedNotClaimedMeasured(t *testing.T){
 examples:=[]struct{input,limit,reserve int;want string}{
  {0,0,0,"unknown"},
  {0,1024,0,"unknown"},
  {600,1024,0,"normal"},
  {725,1024,0,"checkpoint_recommended"},
  {900,1024,0,"handoff_recommended"},
 }
 for _,tc:=range examples{
  got:=contextPressure(ContinuityCheckpoint{
   ReportedInputTokens:tc.input,ContextWindowTokens:tc.limit,
   ReservedOutputTokens:tc.reserve,
  })
  if got.Status!=tc.want{t.Fatalf("unexpected pressure: %+v",got)}
  if tc.limit>0&&got.Source!="caller_reported_unverified"{
   t.Fatal("reported token usage was described as authoritative")
  }
 }
}
