package health

import(
 "context"
 "errors"
 "testing"
 "time"
)
func TestFoodProposalsRequireConfirmationAndAvoidDuplicateLogging(t *testing.T){
 s:=testStore(t)
 ctx:=context.Background()
 p,err:=s.SaveFoodProposal(ctx,FoodProposal{
  Food:"Oats",Portion:"40 grams",Origin:"photo_estimate",ObservedAt:time.Now(),
  Nutrients:map[string]float64{"protein":6.5,"fiber":4.0},
  Units:map[string]string{"protein":"g","fiber":"g"},
 })
 if err!=nil{t.Fatal(err)}
 records,err:=s.List(ctx,"nutrition","",10,false)
 if err!=nil||len(records)!=0{t.Fatalf("unconfirmed food logged: %+v %v",records,err)}
 n,err:=s.ConfirmFoodProposal(ctx,p.ID)
 if err!=nil||n!=2{t.Fatalf("confirm: %d %v",n,err)}
 n,err=s.ConfirmFoodProposal(ctx,p.ID)
 if err!=nil||n!=0{t.Fatalf("idempotence: %d %v",n,err)}
 records,err=s.List(ctx,"nutrition","",10,false)
 if err!=nil||len(records)!=2{t.Fatalf("confirmed food count: %+v %v",records,err)}
 for _,record:=range records{
  if record.Method!="estimated"{t.Fatalf("visual inference claimed to be measured: %+v",record)}
 }
 saved,err:=s.GetFoodProposal(ctx,p.ID)
 if err!=nil||saved.Status!="confirmed"{t.Fatalf("confirmed status: %+v %v",saved,err)}
}
func TestDiscardedFoodCannotBeRecorded(t *testing.T){
 s:=testStore(t);ctx:=context.Background()
 p,err:=s.SaveFoodProposal(ctx,FoodProposal{
  Food:"Soup",Portion:"1 bowl",Origin:"manual_estimate",ObservedAt:time.Now(),
  Nutrients:map[string]float64{"energy_kcal":120},
  Units:map[string]string{"energy_kcal":"kcal"},
 })
 if err!=nil{t.Fatal(err)}
 if err=s.DiscardFoodProposal(ctx,p.ID);err!=nil{t.Fatal(err)}
 if _,err=s.ConfirmFoodProposal(ctx,p.ID);err==nil{t.Fatal("discarded food was logged")}
 if err=s.DiscardFoodProposal(ctx,p.ID);!errors.Is(err,ErrConflict){t.Fatalf("double discard: %v",err)}
}
