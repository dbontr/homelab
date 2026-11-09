package transport

import (
    "context"
    "strings"
    "testing"

    "github.com/dbontr/homelab-new/system/agentflow"
    "github.com/dbontr/homelab-new/system/store"
)

func TestDelegationDerivesRunEvidenceWithoutCompletingTasks(t *testing.T) {
    server,_,_:=flowFixture(t)
    engine:=&engineeringFixture{run:agentflow.RunSummary{
        ID:"run-test",Project:"study",Status:agentflow.Succeeded,Revision:1,
    }}
    server.SetEngineering(engine)
    task,err:=server.db.AddTask(store.TaskInput{
        Title:"Implement a theorem checker",
        Links:[]string{"homelab:project:study","homelab:engineering_run:run-test","homelab:engineering_run:run-test"},
    })
    if err!=nil{t.Fatal(err)}
    result:=server.taskDelegations()
    if len(result.Delegations)!=1||result.Delegations[0].State!="ready_for_review"||
       result.Delegations[0].RunStatus!=string(agentflow.Succeeded) {
        t.Fatalf("recorded run not reflected: %+v",result)
    }
    record,err:=server.resource(context.Background(),"task",task.ID)
    if err!=nil{t.Fatal(err)}
    if record.Data.(store.Task).Status!="open"{
        t.Fatal("delegation report silently completed a personal task")
    }
    success,err:=server.db.CompleteTask(task.ID)
    if err!=nil||success.Status!="completed"{t.Fatal(err)}
    if state:=server.taskDelegations().Delegations[0].State;state!="aligned"{
        t.Fatalf("successful task and run should agree: %s",state)
    }
    engine.run.Status=agentflow.Failed
    if state:=server.taskDelegations().Delegations[0].State;state!="inconsistent"{
        t.Fatalf("failed linked run should not appear complete: %s",state)
    }
    engine.run.Status=agentflow.Succeeded
    _,err=server.db.AddTask(store.TaskInput{
        Title:"Cross-project mismatch",
        Links:[]string{"homelab:project:other","homelab:engineering_run:run-test"},
    })
    if err!=nil{t.Fatal(err)}
    records:=server.taskDelegations().Delegations
    if len(records)!=2 {t.Fatalf("expected two linked tasks: %+v",records)}
    conflict:=false
    for _,record:=range records {
        if record.TaskTitle=="Cross-project mismatch"{conflict=record.State=="scope_conflict"}
    }
    if !conflict{t.Fatalf("project identity conflict ignored: %+v",records)}
    server.SetEngineering(nil)
    for _,record:=range server.taskDelegations().Delegations{
        if record.State!="unknown"{t.Fatalf("offline engineering fabricated status: %+v",record)}
    }
}

func TestTaskLinksAreBoundedAndDelegationAPIRequiresAuthentication(t *testing.T) {
    server,_,_:=flowFixture(t)
    h:=server.Handler()
    if r:=request(h,"GET","/v1/tasks/delegations","","");r.Code!=401{
        t.Fatalf("anonymous delegation read: %d",r.Code)
    }
    if r:=request(h,"GET","/v1/tasks/delegations","",strings.Repeat("z",40));r.Code!=200{
        t.Fatalf("owner delegation read: %d",r.Code)
    }
    huge:=strings.Repeat("a",513)
    if _,err:=server.db.AddTask(store.TaskInput{Title:"Bad links",Links:[]string{huge}});err==nil{
        t.Fatal("oversized task link accepted")
    }
    links:=make([]string,33)
    for i:=range links{links[i]="homelab:project:study"}
    if _,err:=server.db.AddTask(store.TaskInput{Title:"Too many",Links:links});err==nil{
        t.Fatal("unbounded task links accepted")
    }
}
