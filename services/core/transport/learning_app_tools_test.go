package transport

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLearningLibraryHasOnlyExplicitReadOperations(t *testing.T){
	for name:=range ownerLearningReadTools{
		path:="/v1/learning/app-tools/"+name+"/call"
		if !ownerLearningReadPathAllowed(path){t.Errorf("missing learning inspection: %s",name)}
	}
	for _,path:=range []string{
		"/v1/learning/app-tools/grade_learning_question/call",
		"/v1/learning/app-tools/get_problem_solution/call",
		"/v1/learning/app-tools/answer_learning_question/call",
		"/v1/learning/app-tools/upload_attachment/call",
		"/v1/learning/app-tools/save_problem_draft/call",
		"/v1/learning/app-tools/create_practice_problem/call",
		"/v1/learning/app-tools/get_graph/extra/call",
		"/v1/learning/app-tools/../get_graph/call",
		"/v1/tools/learning__get_graph/call",
	}{
		if ownerLearningReadPathAllowed(path){t.Errorf("unapproved learning tool: %s",path)}
	}
}

func TestLearningLibraryRejectsUnknownToolBeforeInvocation(t *testing.T){
	s:=&Server{}
	for _,name:=range []string{"get_problem_solution","grade_learning_question","security_session_open"}{
		req:=httptest.NewRequest(http.MethodPost,"/v1/learning/app-tools/"+name+"/call",
			strings.NewReader(`{"arguments":{}}`))
		req.SetPathValue("name",name)
		rec:=httptest.NewRecorder()
		s.uiLearningReadTool(rec,req)
		if rec.Code!=404{t.Fatalf("%q returned %d",name,rec.Code)}
	}
}
