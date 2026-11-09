package security

import (
 "context"
 "encoding/json"
 "errors"
)

// Handler must be registered by the separate executor, not supplied by a tool call.
type Handler func(context.Context,string,json.RawMessage)error

type Executor struct {
 authority *Authority
 handlers map[string]Handler
}

func NewExecutor(authority *Authority,handlers map[string]Handler)(*Executor,error){
 if authority==nil||len(handlers)==0{return nil,errors.New("executor requires authorization and explicit handlers")}
 copy:=make(map[string]Handler,len(handlers))
 for name,handler:=range handlers {
  if name==""||handler==nil{return nil,errors.New("invalid executor handler")}
  copy[name]=handler
 }
 return &Executor{authority:authority,handlers:copy},nil
}

func(e *Executor)Execute(ctx context.Context,grant,operation,target string,args json.RawMessage)error{
 handler,ok:=e.handlers[operation]
 if !ok{return errors.New("operation is not registered with executor")}
 if err:=e.authority.Consume(grant,operation,target,args);err!=nil{return err}
 return handler(ctx,target,args)
}
