package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, e := Open(filepath.Join(t.TempDir(), "learning.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func must(t *testing.T, v Value, err error) Value {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func checkErr(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
}
func TestConceptLifecycleAndStrictPrerequisites(t *testing.T) {
	s := newTestStore(t)
	if _, e := s.AddConcept("Bad Title", "x", "x", ""); e == nil {
		t.Fatal("invalid slug accepted")
	}
	_, e := s.AddConcept("algebra", "Algebra", "Equations", "")
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.AddConcept("calculus", "Calculus", "Rates", "")
	if e != nil {
		t.Fatal(e)
	}
	g, e := s.ConnectConcepts("calculus", "algebra", "requires", "Calculus extends algebra")
	if e != nil {
		t.Fatal(e)
	}
	if g["target"] != "algebra" {
		t.Fatal(g)
	}
	_, e = s.ConnectConcepts("algebra", "calculus", "requires", "cycle")
	checkErr(t, e)
	_, e = s.AddJustification("calculus", "Derivative is a limit", "deductive", nil, "proof", "")
	checkErr(t, e)
	_, e = s.AddJustification("calculus", "Difference quotient", "deductive", []string{"algebra"}, "Approach a limit", "")
	if e != nil {
		t.Fatal(e)
	}
	c, e := s.Concept("calculus")
	if e != nil {
		t.Fatal(e)
	}
	if c["mastery"].(Value)["level"] != 0 {
		t.Fatal("source content granted mastery", c)
	}
	if len(c["justifications"].([]Value)) != 1 {
		t.Fatal(c)
	}
	_, e = s.SetTarget(3)
	if e != nil {
		t.Fatal(e)
	}
	m, e := s.Graph("calculus", 2, 80)
	if e != nil {
		t.Fatal(e)
	}
	if len(m["nodes"].([]Value)) != 2 || len(m["edges"].([]Value)) != 1 {
		t.Fatal(m)
	}
}
func TestEvidenceAndRetention(t *testing.T) {
	s := newTestStore(t)
	_, err := s.AddConcept("series", "Series", "Sequences", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.RecordEvidence("series", "retention", "delayed", "yes", "good", true, .99, false)
	checkErr(t, err)
	_, err = s.RecordEvidence("series", "independent_reconstruction", "proof", "yes", "good", true, .99, true)
	checkErr(t, err)
	for _, d := range dimensions {
		if d == "retention" {
			continue
		}
		_, err = s.RecordEvidence("series", d, "Question", "Answer", "Evaluation", true, .91, false)
		if err != nil {
			t.Fatal(d, err)
		}
	}
	_, err = s.RecordEvidence("series", "retention", "day2", "yes", "good", true, .98, false)
	checkErr(t, err)
	_, err = s.with(func(db *database) Value {
		exec(db, "UPDATE evidence SET recorded_at=? WHERE concept_id=? AND dimension='independent_reconstruction'", time.Now().Add(-26*time.Hour).UTC().Format("2006-01-02T15:04:05.000000+00:00"), "series")
		return Value{}
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.RecordEvidence("series", "retention", "day2", "yes", "good", true, .98, false)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Concept("series")
	if err != nil {
		t.Fatal(err)
	}
	m := c["mastery"].(Value)
	if m["level"] != 5 || m["meets_target"] != false {
		t.Fatal(m)
	}
	_, err = s.RecordEvidence("series", "recognition", "retest", "no", "failed", false, .9, false)
	if err != nil {
		t.Fatal(err)
	}
	c, _ = s.Concept("series")
	if c["mastery"].(Value)["level"] != 0 {
		t.Fatal("latest failed assessment ignored")
	}
}
func TestProblemsAndSolutionSeal(t *testing.T) {
	s := newTestStore(t)
	_, err := s.AddConcept("logic", "Logic", "Deduction", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateProblem("Proof", "Prove it", "proof", "independent", "challenging", []string{"logic"}, "", "generated", "", "", "")
	checkErr(t, err)
	p, err := s.CreateProblem("Proof", "Prove it", "proof", "independent", "challenging", []string{"logic"}, "", "generated", "True by deduction", "Check each inference", "")
	if err != nil {
		t.Fatal(err)
	}
	id := p["id"].(string)
	if strings.Contains(js(p), "True by deduction") {
		t.Fatal("answer leaked")
	}
	_, err = s.GetProblemSolution(id)
	checkErr(t, err)
	d, err := s.SaveProblemDraft(id, "First proof")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.SaveProblemDraft(id, "First proof")
	if err != nil {
		t.Fatal(err)
	}
	if len(d["problem"].(Value)["drafts"].([]Value)) != 1 {
		t.Fatal("duplicate draft")
	}
	p, err = s.FinishProblem(id, "solved", "Correct", .95, []string{"logic"})
	if err != nil {
		t.Fatal(err)
	}
	if p["independently_solved"] != true {
		t.Fatal("missing independent credit")
	}
	solution, err := s.GetProblemSolution(id)
	if err != nil {
		t.Fatal(err)
	}
	if solution["reference_solution"] != "True by deduction" {
		t.Fatal(solution)
	}
	_, err = s.SaveProblemDraft(id, "Later edit")
	checkErr(t, err)
	exam, err := s.CreateProblem("Exam", "Prove", "proof", "exam", "standard", nil, "", "user", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.RecordProblemHint(exam["id"].(string), 0, "hint")
	checkErr(t, err)
	guided, err := s.CreateProblem("Guided", "Prove", "proof", "guided", "standard", nil, "", "user", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	gid := guided["id"].(string)
	_, err = s.SaveProblemDraft(gid, "attempt")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.RecordProblemHint(gid, 1, "Think of compactness")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.RecordProblemHint(gid, 0, "too early")
	checkErr(t, err)
	_, err = s.FinishProblem(gid, "solved", "Correct assisted", .97, nil)
	if err != nil {
		t.Fatal(err)
	}
}
func TestSessionsAndReasoningRoutes(t *testing.T) {
	s := newTestStore(t)
	for _, cid := range []string{"algebra", "limits", "derivatives"} {
		if _, e := s.AddConcept(cid, cid, "foundational summary", ""); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := s.ConnectConcepts("limits", "algebra", "requires", "algebraic expressions"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ConnectConcepts("derivatives", "limits", "requires", "limits define instant rates"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.AddJustification("derivatives", "Derivative definition", "deductive", []string{"limits"}, "Limit the quotient", ""); e != nil {
		t.Fatal(e)
	}
	if _, e := s.AddJustification("limits", "Limit definition", "definitional", []string{"algebra"}, "Bound the error", ""); e != nil {
		t.Fatal(e)
	}
	sess, err := s.StartLearningSession("Reconstruct derivatives", "derivatives", "learn", nil)
	if err != nil {
		t.Fatal(err)
	}
	id := sess["id"].(string)
	probe, err := s.RecommendSessionProbe(id)
	if err != nil {
		t.Fatal(err)
	}
	if probe["action"] != "probe" || probe["concept_id"] != "derivatives" {
		t.Fatal(probe)
	}
	_, err = s.RecordSessionProbe(id, "derivatives", "Why define a derivative?", "Need speed", "Gap", "gap", .90, false)
	if err != nil {
		t.Fatal(err)
	}
	probe, err = s.RecommendSessionProbe(id)
	if err != nil {
		t.Fatal(err)
	}
	if probe["concept_id"] != "limits" {
		t.Fatal(probe)
	}
	_, err = s.AdvanceLearningSession(id, "plan", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.AdvanceLearningSession(id, "teach", nil)
	checkErr(t, err)
	plan, err := s.BuildLearningPlan(id, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	result := plan["session"].(Value)
	steps := result["steps"].([]Value)
	if len(steps) < 3 {
		t.Fatal("incomplete route", plan)
	}
	stale := 0
	_, err = s.AdvanceLearningSession(id, "teach", &stale)
	checkErr(t, err)
	_, err = s.AdvanceLearningSession(id, "teach", nil)
	if err != nil {
		t.Fatal(err)
	}
	first := steps[0]["id"].(string)
	_, err = s.RecordLearningStep(id, first, "my reasoning", "Valid provisional", "supported", .94, false)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := s.BuildLearningPlan(id, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p2["plan_version"] != 2 {
		t.Fatal(p2)
	}
	_, err = s.AdvanceLearningSession(id, "practice", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.AdvanceLearningSession(id, "reassess", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.RecordSessionProbe(id, "limits", "Retest", "x", "Gap", "uncertain", .42, false)
	if err != nil {
		t.Fatal(err)
	}
	info, err := s.GetLearningSession(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(info["probes"].([]Value)) != 2 || len(info["plan_history"].([]Value)) != 2 {
		t.Fatal(info)
	}
}
func TestConcurrentWritesAndPersistentCompatibility(t *testing.T) {
	s := newTestStore(t)
	var wg sync.WaitGroup
	errs := make(chan error, 120)
	for worker := 0; worker < 3; worker++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for i := 0; i < 40; i++ {
				_, err := s.AddConcept(fmtID(n, i), "Number", "Summary", "")
				if err != nil {
					errs <- err
					return
				}
			}
		}(worker)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	report, e := s.Summary()
	if e != nil {
		t.Fatal(e)
	}
	if report["concepts"] != 120 {
		t.Fatal(report)
	}
	integrity, e := s.with(func(db *database) Value { r := getRow(db, "PRAGMA integrity_check"); return Value(r) })
	if e != nil {
		t.Fatal(e)
	}
	if integrity["integrity_check"] != "ok" {
		t.Fatal(integrity)
	}
}
func fmtID(n, i int) string { return fmt.Sprintf("worker-%d-concept-%d", n, i) }

func TestLegacySQLiteSnapshot(t *testing.T) {
	fixture, err := os.ReadFile("testdata/legacy-schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "history.db")
	sqlite, err := openDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlite.run("PRAGMA foreign_keys=OFF"); err != nil {
		t.Fatal(err)
	}
	if err := sqlite.script(string(fixture)); err != nil {
		t.Fatal(err)
	}
	if err := sqlite.run("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	if err := sqlite.close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	summary, err := s.Summary()
	if err != nil {
		t.Fatal(err)
	}
	if summary["concepts"] != 3 || summary["open_problems"] != 1 {
		t.Fatal(summary)
	}
	c, err := s.Concept("derivatives")
	if err != nil {
		t.Fatal(err)
	}
	if len(c["justifications"].([]Value)) != 1 || len(c["prerequisites"].([]Value)) != 1 {
		t.Fatal(c)
	}
	sessions, err := s.ListLearningSessions(false, 10)
	if err != nil {
		t.Fatal(err)
	}
	current := sessions["sessions"].([]Value)[0]
	if len(current["steps"].([]Value)) == 0 {
		t.Fatal("missing session route")
	}

	full, err := s.GetLearningSession(current["id"].(string))
	if err != nil || len(full["probes"].([]Value)) != 1 {
		t.Fatal(full, err)
	}
	problems, err := s.ListProblems("open", 10)
	if err != nil {
		t.Fatal(err)
	}
	p := problems["problems"].([]Value)[0]
	if strings.Contains(js(p), "nested compact sets") {
		t.Fatal("reference answer leaked")
	}
	_, err = s.GetProblemSolution(p["id"].(string))
	checkErr(t, err)
	_, err = s.SaveProblemDraft(p["id"].(string), "A better compactness argument")
	if err != nil {
		t.Fatal(err)
	}
}
