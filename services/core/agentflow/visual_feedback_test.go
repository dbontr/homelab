package agentflow

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/base64"
 "encoding/hex"
 "encoding/json"
 "image"
 "image/png"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
)

func visualPNG(t *testing.T) ProjectPNG {
 t.Helper()
 var data bytes.Buffer
 if err:=png.Encode(&data,image.NewRGBA(image.Rect(0,0,64,48)));err!=nil{t.Fatal(err)}
 digest:=sha256.Sum256(data.Bytes())
 return ProjectPNG{ArtifactID:strings.Repeat("a",32),Data:data.Bytes(),SHA256:hex.EncodeToString(digest[:]),Width:64,Height:48}
}

func TestVisualProgramRequiresExplicitOptInAndScope(t *testing.T){
 good:=Program{
  Project:"demo",Repository:"acme/demo",Revision:strings.Repeat("b",40),
  ExpectedHead:strings.Repeat("b",40),BaseBranch:"main",
  Objective:"Inspect a rendered layout",ImagePoolID:"visual-approved",
  CPU:1,MemoryMB:1024,DurationMinutes:30,Title:"Visual QA",
  Checks:[]string{"make test"},AllowedPaths:[]string{"web/index.html"},Autonomous:true,
  VisualFeedback:true,VisualPaths:[]string{"artifacts/site.png"},VirtualDisplay:false,
  OutputPaths:[]string{"photos/result.jpg","photos/photo.CR3.xmp","photos/photo.CR3"},
 }
 if err:=good.validate();err!=nil{t.Fatal(err)}
 compiled,err:=Compile(good)
 if err!=nil{t.Fatal(err)}
 if compiled.Inputs==nil||!compiled.Inputs.VisualFeedback||len(compiled.Inputs.OutputPaths)!=3{t.Fatal("visual and output scopes missing from run identity")}
 deliver:=false
 for _,step:=range compiled.Steps {if step.Kind=="deliver"{deliver=true}}
 if !deliver{t.Fatal("deliverables must compile into a durable step")}
 bad:=good
 bad.VisualFeedback=false
 if err:=bad.validate();err==nil{t.Fatal("visual image paths require opt-in")}
 bad=good
 bad.Autonomous=false
 if err:=bad.validate();err==nil{t.Fatal("visual feedback requires autonomous action policy")}
 bad=good
 bad.VisualPaths=[]string{"../../../secret.png"}
 if err:=bad.validate();err==nil{t.Fatal("escaped visual scope accepted")}
 bad=good
 bad.VisualPaths=[]string{"config/.env.png"}
 if err:=bad.validate();err==nil{t.Fatal("sensitive image scope accepted")}
 brief:=AgentBrief{VisualFeedback:true,VisualPaths:[]string{"artifacts/site.png"},VirtualDisplay:false}
 for _,action:=range []AgentAction{{Action:"observe",Path:"artifacts/unapproved.png"},{Action:"observe"},{Action:"observe",Path:"../secret.png"}}{
  if err:=action.validate(brief);err==nil{t.Fatalf("unapproved image observation accepted: %+v",action)}
 }
 if err:=(AgentAction{Action:"observe",Path:"artifacts/site.png"}).validate(brief);err!=nil{t.Fatal(err)}
}

func TestVisualModelSeesVerifiedImageAndReturnsBoundedFeedback(t *testing.T){
 photo:=visualPNG(t)
 seen:=false
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  var request struct{Messages []struct{Content json.RawMessage `json:"content"`} `json:"messages"`}
  if err:=json.NewDecoder(r.Body).Decode(&request);err!=nil{t.Error(err);w.WriteHeader(400);return}
  if len(request.Messages)!=2 {t.Error("missing model context");w.WriteHeader(400);return}
  var parts []struct{Type string `json:"type"`;ImageURL struct{URL string `json:"url"`} `json:"image_url"`}
  if err:=json.Unmarshal(request.Messages[1].Content,&parts);err!=nil{t.Error(err);w.WriteHeader(400);return}
  if len(parts)!=2||parts[0].Type!="text"||parts[1].Type!="image_url"||parts[1].ImageURL.URL!="data:image/png;base64,"+base64.StdEncoding.EncodeToString(photo.Data){
   t.Error("model did not receive expected in-memory PNG");w.WriteHeader(400);return
  }
  seen=true
  reply:=map[string]any{"choices":[]map[string]any{{"message":map[string]any{"content":`{"status":"needs_revision","issues":["Overlapping labels"],"suggestions":["Separate the two labels"]}`},"finish_reason":"stop"}}}
  _=json.NewEncoder(w).Encode(reply)
 }))
 defer server.Close()
 model,err:=NewModelAuthor(server.URL+"/v1/chat/completions","vision","")
 if err!=nil{t.Fatal(err)}
 result,err:=model.ReviewVisual(context.Background(),AgentBrief{
  VisualFeedback:true,Objective:"Fix overlapping labels",Repository:"acme/demo",
  AllowedPaths:[]string{"circuit.asc"},
 },photo)
 if err!=nil||result.Status!="needs_revision"||!seen{t.Fatalf("visual feedback failed: %v %+v",err,result)}
 if _,err:=model.ReviewVisual(context.Background(),AgentBrief{},photo);err==nil{t.Fatal("missing explicit opt-in accepted")}
}

type visualComputeMock struct{picture ProjectPNG}
func(v *visualComputeMock)Call(_ context.Context,tool string,_ map[string]any)(map[string]any,error){
 switch tool {
 case "workspace_call":return map[string]any{"operation_id":strings.Repeat("a",32)},nil
 case "workspace_operation":return map[string]any{"status":"succeeded","result":map[string]any{"artifact_id":v.picture.ArtifactID}},nil
 default:return nil,context.Canceled
 }
}
func(v *visualComputeMock)ReadProjectPNG(_ context.Context,_ string,id string)(ProjectPNG,error){
 if id!=v.picture.ArtifactID{return ProjectPNG{},context.Canceled}
 return v.picture,nil
}
type visualAuthorMock struct{}
func(visualAuthorMock)Propose(context.Context,string,map[string]string,[]string)(map[string]string,error){return nil,nil}
func(visualAuthorMock)ReviewVisual(context.Context,AgentBrief,ProjectPNG)(VisualAssessment,error){
 return VisualAssessment{Status:"acceptable",Suggestions:[]string{"Layout is readable"}},nil
}
func TestObserveWorkspacePreservesImageReceiptAndAcceptance(t *testing.T){
 picture:=visualPNG(t)
 engine:=&Engine{Compute:&visualComputeMock{picture:picture},Author:visualAuthorMock{}}
 w:=Workflow{Project:"demo",Steps:[]Step{{ID:"provision",Status:Succeeded,Result:json.RawMessage(`{"id":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}`)}}}
 state:=&AgentSession{}
 brief:=AgentBrief{Repository:"acme/demo",VisualFeedback:true,VirtualDisplay:true}
 detail,err:=engine.observeWorkspace(context.Background(),w,brief,state,AgentAction{Action:"observe"})
 if err!=nil||!state.VisualReady||len(state.Visuals)!=1||state.Visuals[0].SHA256!=picture.SHA256||!strings.Contains(detail,picture.ArtifactID){
  t.Fatalf("visual observation not persisted: %v %+v %s",err,state,detail)
 }
 brief.VisualFeedback=false
 if _,err:=engine.observeWorkspace(context.Background(),w,brief,state,AgentAction{Action:"observe"});err==nil {
  t.Fatal("observation without owner opt-in permitted")
 }
}

func TestProjectPNGRejectsCrossProjectAndCorruption(t *testing.T){
 picture:=visualPNG(t)
 damaged:=false
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  var request struct{Params struct{Arguments struct{Project string `json:"project_id"`;ID string `json:"artifact_id"`} `json:"arguments"`} `json:"params"`}
  _=json.NewDecoder(r.Body).Decode(&request)
  if request.Params.Arguments.Project!="demo"||request.Params.Arguments.ID!=picture.ArtifactID {
   _,_=w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"isError":true,"content":[{"type":"text","text":"denied"}]}}`));return
  }
  hash:=picture.SHA256
  if damaged{hash=strings.Repeat("0",64)}
  response:=map[string]any{"jsonrpc":"2.0","id":1,"result":map[string]any{
   "isError":false,
   "structuredContent":map[string]any{"artifact_id":picture.ArtifactID,"mime_type":"image/png","sha256":hash,"width":64,"height":48},
   "content":[]map[string]any{{"type":"image","mimeType":"image/png","data":base64.StdEncoding.EncodeToString(picture.Data)}},
  }}
  _=json.NewEncoder(w).Encode(response)
 }))
 defer server.Close()
 client,err:=NewComputeClient(server.URL+"/mcp",strings.Repeat("a",40))
 if err!=nil{t.Fatal(err)}
 got,err:=client.ReadProjectPNG(context.Background(),"demo",picture.ArtifactID)
 if err!=nil||got.SHA256!=picture.SHA256{t.Fatalf("valid image: %v %+v",err,got)}
 if _,err:=client.ReadProjectPNG(context.Background(),"other",picture.ArtifactID);err==nil{t.Fatal("cross-project image disclosed")}
 damaged=true
 if _,err:=client.ReadProjectPNG(context.Background(),"demo",picture.ArtifactID);err==nil{t.Fatal("corrupt image accepted")}
}
