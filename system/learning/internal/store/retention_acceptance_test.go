package store

import (
	"fmt"
	"testing"
	"time"
)

func acceptanceEvidenceTime(hoursAgo int) string {
	return time.Now().UTC().Add(-time.Duration(hoursAgo) * time.Hour).Format("2006-01-02T15:04:05.000000+00:00")
}

func TestDelayedRetentionRejectsShortcutsAcrossDisciplines(t *testing.T) {
	cases := []struct {
		id, name, domain string
	}{
		{"limits", "Limit construction", "mathematics"},
		{"circuits", "Kirchhoff reasoning", "electrical engineering"},
		{"evolution", "Evolution by natural selection", "biology"},
		{"archives", "Source criticism", "history"},
		{"soldering", "Safe solder joint inspection", "practical craft"},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			mustResult := requirePedagogyValue(t)
			s := newTestStore(t)
			mustResult(s.AddConcept(tc.id, tc.name, "Explain and apply the principle independently", tc.domain))
			for _, dimension := range []string{"recognition", "application", "justification", "transfer", "independent_reconstruction"} {
				mustResult(s.RecordEvidence(tc.id, dimension, "Unseen question", "Independent explanation", "Supported by the evidence", true, .97, false))
			}
			if _, err := s.RecordEvidence(tc.id, "retention", "Immediate repetition", "Same surface response", "Correct", true, .97, false); err == nil {
				t.Fatal("immediate repetition counted as delayed retention")
			}
			if _, err := s.RecordEvidence(tc.id, "retention", "Assisted retest", "Guided answer", "Correct", true, .97, true); err == nil {
				t.Fatal("assisted retention accepted")
			}
			// Simulate legacy database content that bypassed the write-time gate.
			mustResult(s.with(func(db *database) Value {
				exec(db, "INSERT INTO evidence VALUES(?,?,?,?,?,?,?,?,?,?)", uuid(), tc.id, "retention", "Legacy retest", "Recognized answer", "Supported", true, .98, false, now())
				return Value{}
			}))
			if got := mustResult(s.Concept(tc.id))["mastery"].(Value)["level"]; got != 4 {
				t.Fatalf("premature persisted retention raised the level: %v", got)
			}
			mustResult(s.with(func(db *database) Value {
				exec(db, "DELETE FROM evidence WHERE concept_id=? AND dimension='retention'", tc.id)
				exec(db, "UPDATE evidence SET recorded_at=? WHERE concept_id=? AND dimension='independent_reconstruction'", acceptanceEvidenceTime(48), tc.id)
				return Value{}
			}))
			mustResult(s.RecordEvidence(tc.id, "retention", "Novel delayed problem", "Independent reconstruction", "Correct with justification", true, .98, false))
			if got := mustResult(s.Concept(tc.id))["mastery"].(Value)["level"]; got != 5 {
				t.Fatalf("qualified delayed retention was not recognized: %v", got)
			}
			mustResult(s.RecordEvidence(tc.id, "independent_reconstruction", "New proof", "Independent argument", "Supported", true, .98, false))
			if got := mustResult(s.Concept(tc.id))["mastery"].(Value)["level"]; got != 4 {
				t.Fatalf("old retention still counted after a newer reconstruction: %v", got)
			}
			if _, err := s.RecordEvidence(tc.id, "retention", "Immediate repeat", "Independent argument", "Correct", true, .98, false); err == nil {
				t.Fatal("a new reconstruction reused the previous retention interval")
			}
		})
	}
}

func TestDelayedReassessmentQueueRequiresCurrentQualifyingEvidence(t *testing.T) {
	mustResult := requirePedagogyValue(t)
	s := newTestStore(t)
	mustResult(s.AddConcept("vector", "Vector analysis", "Invariant transformations", "physics"))
	mustResult(s.RecordEvidence("vector", "independent_reconstruction", "Rebuild vector addition", "Unassisted work", "Supported", true, .92, false))
	// An immediate assessment does not appear in the delayed queue.
	if due := mustResult(s.DueReassessments(10))["due"].([]Value); len(due) != 0 {
		t.Fatal("immediate retest scheduled as due", due)
	}
	mustResult(s.with(func(db *database) Value {
		exec(db, "UPDATE evidence SET recorded_at=? WHERE concept_id='vector' AND dimension='independent_reconstruction'", acceptanceEvidenceTime(100))
		return Value{}
	}))
	if due := mustResult(s.DueReassessments(10))["due"].([]Value); len(due) != 1 {
		t.Fatal("eligible delayed reassessment not scheduled", due)
	}
	mustResult(s.RecordEvidence("vector", "retention", "Apply rotations to unfamiliar axes", "Independent argument", "Correct", true, .95, false))
	if due := mustResult(s.DueReassessments(10))["due"].([]Value); len(due) != 0 {
		t.Fatal("a fresh validated retention assessment remained due", due)
	}
	// The old retention result must not cover a more recent reconstruction.
	mustResult(s.with(func(db *database) Value {
		exec(db, "UPDATE evidence SET recorded_at=? WHERE concept_id='vector' AND dimension='retention'", acceptanceEvidenceTime(50))
		return Value{}
	}))
	mustResult(s.RecordEvidence("vector", "independent_reconstruction", "Rebuild after new examples", "Fresh independent argument", "Supported", true, .96, false))
	mustResult(s.with(func(db *database) Value {
		exec(db, "UPDATE evidence SET recorded_at=? WHERE concept_id='vector' AND dimension='independent_reconstruction' AND recorded_at>?", acceptanceEvidenceTime(50), acceptanceEvidenceTime(30))
		return Value{}
	}))
	if due := mustResult(s.DueReassessments(10))["due"].([]Value); len(due) != 1 {
		t.Fatal("new reconstruction not reassessed on schedule", due)
	}
	mustResult(s.RecordEvidence("vector", "retention", "Solve delayed novel transfer", "Unassisted reasoning", "Supported", true, .95, false))
	if due := mustResult(s.DueReassessments(10))["due"].([]Value); len(due) != 0 {
		t.Fatal("successful delayed reassessment not cleared", due)
	}
	mustResult(s.RecordEvidence("vector", "retention", "Adversarial delayed probe", "Unjustified reasoning", "Incorrect", false, .95, false))
	if due := mustResult(s.DueReassessments(10))["due"].([]Value); len(due) != 1 {
		t.Fatal("latest failed retention did not reopen the due queue", due)
	}
	_, err := s.DueReassessments(101)
	if err == nil {
		t.Fatal("unbounded reassessment query accepted")
	}
}

func TestLaterFailedReconstructionInvalidatesRetention(t *testing.T) {
	mustResult := requirePedagogyValue(t)
	s := newTestStore(t)
	mustResult(s.AddConcept("inference", "Inference", "Valid steps in reasoning", "logic"))
	for _, d := range []string{"recognition", "application", "justification", "transfer", "independent_reconstruction"} {
		mustResult(s.RecordEvidence("inference", d, "Independent question", "Supported explanation", "Correct", true, .98, false))
	}
	mustResult(s.with(func(db *database) Value {
		exec(db, "UPDATE evidence SET recorded_at=? WHERE dimension='independent_reconstruction'", acceptanceEvidenceTime(48))
		return Value{}
	}))
	mustResult(s.RecordEvidence("inference", "retention", "Delayed transfer", "Independent answer", "Supported", true, .95, false))
	if got := mustResult(s.Concept("inference"))["mastery"].(Value)["level"]; got != 5 {
		t.Fatal(fmt.Sprint("expected level 5, got ", got))
	}
	mustResult(s.RecordEvidence("inference", "independent_reconstruction", "Unfamiliar theorem", "Cannot derive", "Unsupported", false, .97, false))
	if got := mustResult(s.Concept("inference"))["mastery"].(Value)["level"]; got != 4 {
		t.Fatalf("failed reconstruction did not remove retained mastery: %v", got)
	}
	if due := mustResult(s.DueReassessments(10))["due"].([]Value); len(due) != 0 {
		t.Fatal("failed reconstruction must be repaired before delayed reassessment", due)
	}
	if _, err := s.RecordEvidence("inference", "retention", "Reattempt", "Repeated old solution", "Correct", true, .95, false); err == nil {
		t.Fatal("new retention accepted after a failed reconstruction")
	}
}
