package health

import(
 "context"
 "errors"
 "testing"
 "time"
)

func TestTrainingSetsCorrectableAndIdempotent(t *testing.T){
 ctx:=context.Background()
 s:=testStore(t)
 value:=80.0
 initial,err:=s.AddTrainingSet(ctx,TrainingSet{Exercise:"Squat",Activity:"strength",SetNumber:1,
  Repetitions:5,LoadKG:&value,At:time.Now(),Source:"manual",ClientKey:"my-first-set"})
 if err!=nil{t.Fatal(err)}
 if _,err=s.AddTrainingSet(ctx,TrainingSet{Exercise:"Squat",Activity:"strength",SetNumber:1,
  Repetitions:5,LoadKG:&value,At:time.Now(),Source:"manual",ClientKey:"my-first-set"});!errors.Is(err,ErrConflict){
   t.Fatalf("duplicate accepted: %v",err)
 }
 corrected:=initial
 corrected.ID=""
 corrected.Replaces=initial.ID
 corrected.ClientKey=""
 corrected.Repetitions=6
 saved,err:=s.AddTrainingSet(ctx,corrected)
 if err!=nil{t.Fatal(err)}
 current,err:=s.ListTrainingSets(ctx,"","Squat",100,false)
 if err!=nil||len(current)!=1||current[0].ID!=saved.ID||current[0].Repetitions!=6{
  t.Fatalf("current training sets: %+v %v",current,err)
 }
 history,err:=s.ListTrainingSets(ctx,"","Squat",100,true)
 if err!=nil||len(history)!=2{t.Fatalf("training history: %+v %v",history,err)}
 if _,err=s.AddTrainingSet(ctx,corrected);err==nil{t.Fatal("accepted second correction")}
}
func TestCardioSetsRequireActivityEvidence(t *testing.T){
 s:=testStore(t)
 _,err:=s.AddTrainingSet(context.Background(),TrainingSet{
  Exercise:"Running",Activity:"cardio",At:time.Now(),Source:"manual"})
 if err==nil{t.Fatal("empty cardio log accepted")}
 distance:=5000.0
 _,err=s.AddTrainingSet(context.Background(),TrainingSet{
  Exercise:"Running",Activity:"cardio",At:time.Now(),Source:"manual",DistanceMeters:&distance})
 if err!=nil{t.Fatal(err)}
}
