package store

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestDerivativeRouteDoesNotSmuggleInLimits(t *testing.T) {
	s := newExtendedStore(t)
	ok := func(v Value, e error) Value {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	ok(s.AddConcept("algebra", "Algebra", "Equations and manipulation", "mathematics"))
	ok(s.AddConcept("limits", "Limits", "Approaching a value arbitrarily closely", "analysis"))
	ok(s.AddConcept("derivatives", "Derivatives", "Instantaneous rates of change", "analysis"))
	ok(s.ConnectConcepts("derivatives", "algebra", "requires", "Difference quotients use algebra"))
	ok(s.ConnectConcepts("derivatives", "limits", "requires", "Instantaneous rates require a limiting process"))
	ok(s.AddJustification("derivatives", "The derivative is a limit of difference quotients", "deductive", []string{"algebra", "limits"}, "The limit of the secant slope, if it exists, defines the derivative.", ""))
	ses := ok(s.StartLearningSession("Reconstruct differentiation from secure foundations", "derivatives", "learn", nil))
	sid := ses["id"].(string)
	q := ok(s.OpenLearningQuestion(sid, "derivatives", "", "diagnostic", "Can algebra alone produce an instantaneous derivative?", "multiple_choice", []QuestionOption{{ID: "yes", Label: "Yes"}, {ID: "no", Label: "Not without a limit"}}, "no", "A limiting construction is essential."))
	qid := q["id"].(string)
	ok(s.AnswerLearningQuestion(qid, "no", false))
	ok(s.GradeLearningQuestion(qid, "supported", "Correct, though this is not yet a proof.", .88))
	ok(s.AdvanceLearningSession(sid, "plan", nil))
	plan := ok(s.BuildLearningPlan(sid, nil, nil))
	session := plan["session"].(Value)
	steps := session["steps"].([]Value)
	var algebra, limits, derivatives, link bool
	for _, step := range steps {
		cid := step["concept_id"].(string)
		if cid == "algebra" {
			algebra = true
		}
		if cid == "limits" {
			limits = true
		}
		if cid == "derivatives" {
			derivatives = true
		}
		if step["kind"] == "inference" && cid == "derivatives" {
			link = true
		}
	}
	if !algebra || !limits || !derivatives || !link {
		t.Fatal("derivative route omitted essential foundation or explanatory link", steps)
	}
	if session["plan_ready"] == true {
		t.Fatal("no independent inference has yet been demonstrated", session)
	}
	derivative := ok(s.Concept("derivatives"))
	if derivative["mastery"].(Value)["level"] != 0 {
		t.Fatal("a lesson plan awarded subject mastery")
	}
}

func TestSubjectIndependentPracticalWorkHasOriginalAndNoCredit(t *testing.T) {
	s := newExtendedStore(t)
	ok := func(v Value, e error) Value {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	ok(s.AddConcept("circuit-layout", "Circuit layout", "Physical components connected in a circuit", "engineering"))
	ses := ok(s.StartLearningSession("Understand the measured circuit", "circuit-layout", "learn", nil))
	sessionID := ses["id"].(string)
	img := []byte("\x89PNG\r\n\x1a\n" + "example schematic")

	attach := ok(s.UploadAttachment("session", sessionID, "circuit.png", "image/png", base64.StdEncoding.EncodeToString(img), "Photographed circuit"))
	a := ok(s.AnnotateAttachment(attach["id"].(string), "Wire may connect input pin", "model_interpretation", "Connection partly obscured"))
	if a["mastery_unchanged"] != true {
		t.Fatal(a)
	}
	source := ok(s.IngestText("Lab observations", "observation", "text/plain", "A current was observed across an unknown resistor."))
	span := ok(s.NextExtraction(source["id"].(string)))
	candidate := ok(s.ProposeExtraction(span["span_id"].(string), ExtractionProposal{Claims: []ExtractionCandidate{{Statement: "A current was observed", Quote: "A current was observed", Context: "lab", Domain: "engineering"}}}))
	ok(s.AuditExtraction(candidate["batch_id"].(string), []ExtractionDecision{{Index: 0, Action: "accept"}}, nil))
	rec := ok(s.GetAttachment(attach["id"].(string), false))
	if strings.Contains(js(rec), "image pixels") || rec["mime_type"] != "image/png" {
		t.Fatal("unexpected image read", rec)
	}
	c := ok(s.Concept("circuit-layout"))
	if c["mastery"].(Value)["level"] != 0 {
		t.Fatal("photo or lab data automatically granted learner mastery")
	}
}

func TestCompetingDomainClaimsRemainContextualAndAuditable(t *testing.T) {
	s := newExtendedStore(t)
	ok := func(v Value, e error) Value {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	s1 := ok(s.IngestText("Mechanics", "notes", "text/plain", "Classical models describe gravitation as a force."))
	s2 := ok(s.IngestText("Relativity", "notes", "text/plain", "General relativity describes free fall through spacetime geometry."))
	for _, pair := range []struct {
		source                    Value
		statement, quote, context string
	}{
		{s1, "Gravitation is a force", "gravitation as a force", "classical mechanics"},
		{s2, "Gravitation is modeled geometrically", "free fall through spacetime geometry", "general relativity"},
	} {
		nxt := ok(s.NextExtraction(pair.source["id"].(string)))
		batch := ok(s.ProposeExtraction(nxt["span_id"].(string), ExtractionProposal{Claims: []ExtractionCandidate{{Statement: pair.statement, Quote: pair.quote, Domain: "physics", Context: pair.context}}}))
		ok(s.AuditExtraction(batch["batch_id"].(string), []ExtractionDecision{{Index: 0, Action: "accept"}}, nil))
	}
	graph := ok(s.ReasoningGraph(50))
	claims := graph["propositions"].([]Value)
	if len(claims) != 2 {
		t.Fatal(graph)
	}
	d := ok(s.RecordPropositionDispute(claims[0]["id"].(string), claims[1]["id"].(string), "Apparently incompatible physical descriptions require contextual assessment"))
	duplicate := ok(s.RecordPropositionDispute(claims[1]["id"].(string), claims[0]["id"].(string), "Same pair reversed"))
	if d["id"] != duplicate["id"] {
		t.Fatal("dispute pair did not deduplicate")
	}
	before := ok(s.ReasoningGraph(50))
	if len(before["disputes"].([]Value)) != 1 {
		t.Fatal(before)
	}
	resolved := ok(s.ResolvePropositionDispute(d["id"].(string), "different_contexts", "These are distinct theoretical frameworks"))
	if resolved["status"] != "different_contexts" || resolved["mastery_unchanged"] != true {
		t.Fatal(resolved)
	}
	_, err := s.ResolvePropositionDispute(d["id"].(string), "both_true", "Invalid status")
	checkErr(t, err)
}
