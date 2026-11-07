package mcp

import (
 "context"
 "encoding/json"
 "errors"
)

func (s *Server) registerRescheduling() {
 s.add("health_workout_reschedule","Move an explicitly confirmed missed workout to a later, unoccupied date. An unlogged workout cannot be rescheduled automatically.",false,false,[]string{"plan_id","session_id","target_date","expected_revision"},
 map[string]any{
  "plan_id":field("string","Existing workout plan ID"),
  "session_id":field("string","Confirmed missed session ID"),
  "target_date":field("string","Later calendar date YYYY-MM-DD"),
  "expected_revision":field("integer","Current plan revision, for conflict protection"),
 },
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  planID,err:=requiredString(a,"plan_id");if err!=nil{return nil,err}
  sessionID,err:=requiredString(a,"session_id");if err!=nil{return nil,err}
  date,err:=requiredString(a,"target_date");if err!=nil{return nil,err}
  var revision int
  if json.Unmarshal(a["expected_revision"],&revision)!=nil||revision<1{return nil,errors.New("expected_revision must be positive")}
  plan,err:=s.health.RescheduleConfirmedSession(ctx,planID,sessionID,date,revision)
  if err!=nil{return nil,err}
  return send(plan)
 })
}
