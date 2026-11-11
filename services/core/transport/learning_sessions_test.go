package transport

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"
)

func TestLearningQuestionAnswerPreservesConfidenceAndReasoning(t *testing.T) {
    submitted := make(chan map[string]any, 4)
    backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        var call rpcRequest
        if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
            t.Error(err)
            w.WriteHeader(http.StatusBadRequest)
            return
        }
        w.Header().Set("Content-Type", "application/json")
        switch call.Method {
        case "tools/list":
            _, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"answer_learning_question"}]}}`))
        case "tools/call":
            var args struct {
                Name string `json:"name"`
                Arguments map[string]any `json:"arguments"`
            }
            if err := json.Unmarshal(call.Params, &args); err != nil || args.Name != "answer_learning_question" {
                t.Errorf("unexpected Learning backend operation: %s, %v", call.Params, err)
                w.WriteHeader(http.StatusBadRequest)
                return
            }
            submitted <- args.Arguments
            _, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"{\"status\":\"answered\"}"}]}}`))
        default:
            t.Errorf("unexpected JSON-RPC method: %s", call.Method)
            w.WriteHeader(http.StatusBadRequest)
        }
    }))
    defer backend.Close()

    token := strings.Repeat("a", 40)
    handler := makeTestServer(t, []Backend{{Name:"learning", URL:backend.URL, Token:strings.Repeat("s", 32)}})
    path := "/v1/learning/questions/question-1/answer"
    answer := request(handler, "POST", path,
        `{"learner_response":"option-b","learner_reasoning":"Because the hypothesis requires this assumption.","assisted":true,"self_confidence":0.75}`, token)
    if answer.Code != http.StatusOK {
        t.Fatalf("Learning question submission rejected: %d %s", answer.Code, answer.Body.String())
    }
    params := <-submitted
    if params["question_id"] != "question-1" || params["learner_response"] != "option-b" ||
        params["learner_reasoning"] != "Because the hypothesis requires this assumption." ||
        params["assisted"] != true || params["self_confidence"] != float64(0.75) {
        t.Fatalf("Learning response lost owner-authored evidence: %+v", params)
    }

    withoutConfidence := request(handler,"POST",path,`{"learner_response":"second attempt","assisted":false}`,token)
    if withoutConfidence.Code != http.StatusOK {
        t.Fatalf("Optional confidence incorrectly required: %d %s", withoutConfidence.Code, withoutConfidence.Body.String())
    }
    params = <-submitted
    if _, present := params["self_confidence"]; present {
        t.Fatalf("Unknown confidence was silently replaced with a value: %+v", params)
    }
    if params["learner_response"] != "second attempt" || params["assisted"] != false {
        t.Fatalf("Optional Learning response data changed: %+v", params)
    }

    for _, body := range []string{
        `{"learner_response":"answer","self_confidence":-0.1}`,
        `{"learner_response":"answer","self_confidence":1.01}`,
        `{"learner_response":"answer","self_confidence":"certain"}`,
        `{"learner_response":"   ","self_confidence":0.5}`,
        `{"learner_response":"answer","learner_reasoning":"`+strings.Repeat("a",16001)+`"}`,
    } {
        result := request(handler,"POST",path,body,token)
        if result.Code != http.StatusBadRequest {
            t.Errorf("Invalid Learning response reached backend: %d %s",result.Code, result.Body.String())
        }
    }
    if len(submitted) != 0 {
        t.Fatal("Invalid Learning response was forwarded to the backend")
    }
}
