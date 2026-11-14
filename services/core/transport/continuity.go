package transport

import (
 "bytes"
 "encoding/json"
 "errors"
 "io"

 "github.com/dbontr/homelab-new/system/store"
)

// Continuity tools never infer host context size and never mutate ChatGPT's
// conversation. They record only caller-supplied factual state and usage.
func(s *Server)continuityTool(name string,raw json.RawMessage)(any,error){
 if s==nil||s.db==nil{return nil,errors.New("continuity state store unavailable")}
 if len(raw)>64<<10{return nil,errors.New("continuity request exceeds size limit")}
 decoder:=json.NewDecoder(bytes.NewReader(raw))
 decoder.DisallowUnknownFields()
 switch name{
 case "continuity_checkpoint":
  var input store.ContinuityInput
  if err:=decoder.Decode(&input);err!=nil{return nil,errors.New("invalid continuity checkpoint")}
  var trailing any
  if err:=decoder.Decode(&trailing);err!=io.EOF{
   return nil,errors.New("continuity checkpoint contains trailing JSON")
  }
  checkpoint,pressure,err:=s.db.SaveContinuity(input)
  if err!=nil{return nil,err}
  return map[string]any{
   "checkpoint":checkpoint,"context_pressure":pressure,
   "handoff_available":true,"automatic_chat_transfer":false,
  },nil
 case "continuity_status","continuity_handoff":
  var input struct{Key string `json:"key"`}
  if err:=decoder.Decode(&input);err!=nil{return nil,errors.New("invalid continuity key")}
  var trailing any
  if err:=decoder.Decode(&trailing);err!=io.EOF{
   return nil,errors.New("continuity request contains trailing JSON")
  }
  if name=="continuity_handoff"{
   return s.db.ContinuityHandoff(input.Key)
  }
  checkpoint,pressure,err:=s.db.Continuity(input.Key)
  if err!=nil{return nil,err}
  return map[string]any{
   "checkpoint":checkpoint,"context_pressure":pressure,
   "handoff_available":true,"automatic_chat_transfer":false,
  },nil
 default:
  return nil,errors.New("unknown continuity operation")
 }
}
