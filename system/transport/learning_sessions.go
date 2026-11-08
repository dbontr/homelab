package transport

import (
 "encoding/json"
 "net/http"
 "regexp"
 "strings"
)

var learningID=regexp.MustCompile("^[a-zA-Z0-9_-]{1,128}$")

func (s *Server) learningCall(w http.ResponseWriter,r *http.Request,tool string,payload any,status int){
 raw,err:=json.Marshal(payload)
 if err!=nil{writeError(w,400,"invalid request");return}
 result:=s.invoke(r.Context(),"learning__"+tool,raw,json.RawMessage("1"))
 if failed,_:=result["isError"].(bool);failed{
  writeError(w,502,"learning service did not complete the requested operation");return
 }
 writeJSON(w,status,result)
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
  Assisted bool `json:"assisted"`
 }
 if err:=decode(r,&in);err!=nil{writeError(w,400,err.Error());return}
 if strings.TrimSpace(in.LearnerResponse)==""||len(in.LearnerResponse)>12000||len(in.LearnerReasoning)>16000{
  writeError(w,400,"response and reasoning must be within accepted bounds");return
 }
 s.learningCall(w,r,"answer_learning_question",map[string]any{
  "question_id":id,"learner_response":in.LearnerResponse,
  "learner_reasoning":in.LearnerReasoning,"assisted":in.Assisted},200)
}
