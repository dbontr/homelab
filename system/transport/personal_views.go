package transport

import (
 "net/http"
)

// These views intentionally expose read-only tool results. Privileged MCP
// operations cannot be reached through an owner browser session.
func (s *Server) uiLearningSummary(w http.ResponseWriter,r *http.Request){
 s.uiBackendRead(w,r,"learning","get_learning_summary")
}
func (s *Server) uiLearningSessions(w http.ResponseWriter,r *http.Request){
 s.uiBackendRead(w,r,"learning","list_learning_sessions")
}
func (s *Server) uiLearningReviews(w http.ResponseWriter,r *http.Request){
 s.uiBackendRead(w,r,"learning","get_review_queue")
}
func (s *Server) uiHealthPlans(w http.ResponseWriter,r *http.Request){
 s.uiBackendRead(w,r,"health","health_plan_list")
}
func (s *Server) uiHealthRecords(w http.ResponseWriter,r *http.Request){
 s.uiBackendRead(w,r,"health","health_record_list")
}
