package store

import (
 "strings"
 "testing"
)

func TestWeeklyPlanUsesAbsoluteTimeForOffsetDeadlines(t *testing.T) {
 s,_:=testStore(t)
 later,err:=s.AddTask(TaskInput{
  Title:"Later instant", Minutes:60, Due:"2032-06-08T08:00:00-05:00",
 })
 if err!=nil{t.Fatal(err)}
 earlier,err:=s.AddTask(TaskInput{
  Title:"Earlier instant", Minutes:60, Due:"2032-06-08T18:00:00+10:00",
 })
 if err!=nil{t.Fatal(err)}
 plan,err:=s.PlanWeekWithCapacities("2032-06-07",
  [7]int{60,0,0,0,0,0,0},9,nil)
 if err!=nil{t.Fatal(err)}
 if len(plan.Slots)!=1||plan.Slots[0].TaskID!=earlier.ID||
  plan.Slots[0].DeadlineStatus!="on_time" {
  t.Fatalf("deadline instants were sorted lexically: %+v",plan)
 }
 if len(plan.Unplanned)!=1||plan.Unplanned[0].TaskID!=later.ID{
  t.Fatalf("later deadline was not left unplanned: %+v",plan)
 }
}

func TestWeeklyPlanSortsDateOnlyEndOfLocalDayAgainstTimestamp(t *testing.T) {
 s,_:=testStore(t)
 dateOnly,err:=s.AddTask(TaskInput{
  Title:"End of Monday",Minutes:60,Due:"2032-06-07",
 })
 if err!=nil{t.Fatal(err)}
 instant,err:=s.AddTask(TaskInput{
  Title:"Earlier exact time",Minutes:60,Due:"2032-06-08T00:00:00Z",
 })
 if err!=nil{t.Fatal(err)}
 plan,err:=s.PlanWeekWithCapacities("2032-06-07",
  [7]int{60,0,0,0,0,0,0},9,nil)
 if err!=nil{t.Fatal(err)}
 if len(plan.Slots)!=1||plan.Slots[0].TaskID!=instant.ID {
  t.Fatalf("date-only deadline outranked earlier instant: %+v",plan)
 }
 if len(plan.Unplanned)!=1||plan.Unplanned[0].TaskID!=dateOnly.ID{
  t.Fatalf("date-only task lost: %+v",plan)
 }
}

func TestWeeklyPlanLabelsRecoveryAfterDeadline(t *testing.T) {
 s,_:=testStore(t)
 late,err:=s.AddTask(TaskInput{
  Title:"Overdue paper",Minutes:60,Due:"2032-06-06",
 })
 if err!=nil{t.Fatal(err)}
 onTime,err:=s.AddTask(TaskInput{
  Title:"Later paper",Minutes:60,Due:"2032-06-08",
 })
 if err!=nil{t.Fatal(err)}
 plan,err:=s.PlanWeekWithCapacities("2032-06-07",
  [7]int{180,0,0,0,0,0,0},9,nil)
 if err!=nil{t.Fatal(err)}
 statuses:=map[string]string{}
 for _,slot:=range plan.Slots {statuses[slot.TaskID]=slot.DeadlineStatus}
 if statuses[late.ID]!="late"||statuses[onTime.ID]!="on_time"{
  t.Fatalf("missed deadline was not disclosed: %+v",plan)
 }
 found:=false
 for _,note:=range plan.Limitations {
  if strings.Contains(note,"late recovery"){found=true}
 }
 if !found{t.Fatalf("late timeboxes lacked plan-level warning: %+v",plan)}
}

func TestWeeklyPlanLeavesUndatedTasksWithoutDeadlineClaims(t *testing.T) {
 s,_:=testStore(t)
 task,err:=s.AddTask(TaskInput{Title:"Read papers",Minutes:45})
 if err!=nil{t.Fatal(err)}
 plan,err:=s.PlanWeekWithCapacities("2032-06-07",
  [7]int{60,0,0,0,0,0,0},9,nil)
 if err!=nil{t.Fatal(err)}
 if len(plan.Slots)!=1||plan.Slots[0].TaskID!=task.ID||
  plan.Slots[0].DeadlineStatus!=""{
  t.Fatalf("deadline was inferred from an undated task: %+v",plan)
 }
}
