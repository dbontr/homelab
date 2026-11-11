package transport

import (
 "encoding/json"
 "net/http"
 "regexp"
 "strings"
)

var learningID=regexp.MustCompile("^[a-zA-Z0-9_-]{1,128}$")
var learningCurriculumDigest=regexp.MustCompile("^[a-f0-9]{64}$")

func (s *Server) learningCall(w http.ResponseWriter,r *http.Request,tool string,payload any,status int){
 raw,err:=json.Marshal(payload)
 if err!=nil{writeError(w,400,"invalid request");return}
 result:=s.invoke(r.Context(),"learning__"+tool,raw,json.RawMessage("1"))
 if failed,_:=result["isError"].(bool);failed{
  writeError(w,502,"learning service did not complete the requested operation");return
 }
 parts,ok:=result["content"].([]any)
 if !ok||len(parts)==0{writeError(w,502,"learning service returned no data");return}
 first,ok:=parts[0].(map[string]any)
 if !ok{writeError(w,502,"invalid learning service response");return}
 rawText,ok:=first["text"].(string)
 if !ok||len(rawText)>2<<20{writeError(w,502,"invalid learning service response");return}
 var value any
 if err:=json.Unmarshal([]byte(rawText),&value);err!=nil{
   writeError(w,502,"invalid learning service JSON");return
 }
 writeJSON(w,status,value)
}

func (s *Server) uiLearningStart(w http.ResponseWriter,r *http.Request){
 var in struct{
  Goal string `json:"goal"`
  TargetConceptID string `json:"target_concept_id"`
  Mode string `json:"mode"`
 }
 if err:=decode(r,&in);err!=nil{writeError(w,400,err.Error());return}
 if len(strings.TrimSpace(in.Goal))<2||len(in.Goal)>500||
    !learningID.MatchString(in.TargetConceptID){
  writeError(w,400,"invalid learning goal or concept");return
 }
 if in.Mode==""{in.Mode="learn"}
 if in.Mode!="learn"&&in.Mode!="practise"&&in.Mode!="solve"&&in.Mode!="assess"{
  writeError(w,400,"unsupported learning mode");return
 }
 s.learningCall(w,r,"start_learning_session",map[string]any{
  "goal":in.Goal,"target_concept_id":in.TargetConceptID,"mode":in.Mode},201)
}

func (s *Server) uiLearningSession(w http.ResponseWriter,r *http.Request){
 id:=r.PathValue("id")
 if !learningID.MatchString(id){writeError(w,400,"invalid session ID");return}
 s.learningCall(w,r,"get_learning_session",map[string]string{"session_id":id},200)
}

func (s *Server) uiLearningPending(w http.ResponseWriter,r *http.Request){
 id:=r.PathValue("id")
 if !learningID.MatchString(id){writeError(w,400,"invalid session ID");return}
 s.learningCall(w,r,"get_pending_learning_question",map[string]string{"session_id":id},200)
}

func (s *Server) uiLearningAnswer(w http.ResponseWriter,r *http.Request){
 id:=r.PathValue("id")
 if !learningID.MatchString(id){writeError(w,400,"invalid question ID");return}
 var in struct{
  LearnerResponse string `json:"learner_response"`
  LearnerReasoning string `json:"learner_reasoning"`
  SelfConfidence *float64 `json:"self_confidence,omitempty"`
  Assisted bool `json:"assisted"`
 }
 if err:=decode(r,&in);err!=nil{writeError(w,400,err.Error());return}
 if strings.TrimSpace(in.LearnerResponse)==""||len(in.LearnerResponse)>12000||len(in.LearnerReasoning)>16000{
  writeError(w,400,"response and reasoning must be within accepted bounds");return
 }
 if in.SelfConfidence!=nil && (*in.SelfConfidence<0||*in.SelfConfidence>1){
  writeError(w,400,"self-confidence must be between 0 and 1");return
 }
 args:=map[string]any{
  "question_id":id,"learner_response":in.LearnerResponse,
  "learner_reasoning":in.LearnerReasoning,"assisted":in.Assisted,
 }
 if in.SelfConfidence!=nil{args["self_confidence"]=*in.SelfConfidence}
 s.learningCall(w,r,"answer_learning_question",args,200)
}

func (s *Server) uiLearningSourceCurriculumPreview(w http.ResponseWriter,r *http.Request){
 id:=r.PathValue("id")
 if !learningID.MatchString(id){writeError(w,400,"invalid Learning source ID");return}
 var in struct{Limit int `json:"limit"`}
 if err:=decode(r,&in);err!=nil{writeError(w,400,err.Error());return}
 if in.Limit==0{in.Limit=80}
 if in.Limit<1||in.Limit>200{
  writeError(w,400,"curriculum limit must be between 1 and 200");return
 }
 s.learningCall(w,r,"preview_source_curriculum",map[string]any{
  "source_id":id,"limit":in.Limit},200)
}

func (s *Server) uiLearningSourceCurriculumImport(w http.ResponseWriter,r *http.Request){
 id:=r.PathValue("id")
 if !learningID.MatchString(id){writeError(w,400,"invalid Learning source ID");return}
 var in struct{
  Limit int `json:"limit"`
  ExpectedSHA256 string `json:"expected_sha256"`
  Confirm bool `json:"confirm"`
 }
 if err:=decode(r,&in);err!=nil{writeError(w,400,err.Error());return}
 if in.Limit==0{in.Limit=80}
 if in.Limit<1||in.Limit>200||!learningCurriculumDigest.MatchString(in.ExpectedSHA256)||!in.Confirm{
  writeError(w,400,"curriculum import requires a confirmed source digest and valid selection limit");return
 }
 s.learningCall(w,r,"import_source_curriculum",map[string]any{
  "source_id":id,"limit":in.Limit,
  "expected_sha256":in.ExpectedSHA256,"confirm":true},200)
}
