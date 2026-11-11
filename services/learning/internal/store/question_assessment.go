package store

// QuestionAssessmentMaterial is private grading context. It is never returned
// by the public question or browser resource tools before a grade is recorded.
type QuestionAssessmentMaterial struct {
    QuestionID       string `json:"question_id"`
    SessionID        string `json:"session_id"`
    ConceptID        string `json:"concept_id"`
    Kind             string `json:"kind"`
    Prompt           string `json:"prompt"`
    ResponseKind     string `json:"response_kind"`
    LearnerResponse  string `json:"learner_response"`
    LearnerReasoning string `json:"learner_reasoning"`
    ExpectedAnswer   string `json:"expected_answer"`
    GradingCriteria  string `json:"grading_criteria"`
    Assisted         bool   `json:"assisted"`
}

// QuestionAssessmentContext reads the precommitted answer only after a learner
// has submitted a response. Model proposals have no authority to alter grades.
func (s *Store) QuestionAssessmentContext(questionID string) (QuestionAssessmentMaterial, error) {
    value, err := s.with(func(db *database) Value {
        q := ensureRow(getRow(db, "SELECT * FROM learning_questions WHERE id=?", questionID),
            "learning question", questionID)
        insist(strVal(q["status"]) == "answered",
            "question must have a submitted answer before assessment")
        reflection := getRow(db,
            "SELECT learner_reasoning FROM question_reflections WHERE question_id=?", questionID)
        reasoning := ""
        if reflection != nil {
            reasoning = strVal(reflection["learner_reasoning"])
        }
        return Value{
            "question_id": questionID,
            "session_id": q["session_id"],
            "concept_id": q["concept_id"],
            "kind": q["kind"],
            "prompt": q["prompt"],
            "response_kind": q["response_kind"],
            "learner_response": q["learner_response"],
            "learner_reasoning": reasoning,
            "expected_answer": q["expected_answer"],
            "grading_criteria": q["grading_criteria"],
            "assisted": boolVal(q["assisted"]),
        }
    })
    if err != nil {
        return QuestionAssessmentMaterial{}, err
    }
    return QuestionAssessmentMaterial{
        QuestionID: questionID,
        SessionID: strVal(value["session_id"]),
        ConceptID: strVal(value["concept_id"]),
        Kind: strVal(value["kind"]),
        Prompt: strVal(value["prompt"]),
        ResponseKind: strVal(value["response_kind"]),
        LearnerResponse: strVal(value["learner_response"]),
        LearnerReasoning: strVal(value["learner_reasoning"]),
        ExpectedAnswer: strVal(value["expected_answer"]),
        GradingCriteria: strVal(value["grading_criteria"]),
        Assisted: boolVal(value["assisted"]),
    }, nil
}
