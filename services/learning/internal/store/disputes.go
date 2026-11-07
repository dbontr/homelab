package store

import "strings"

// A source disagreement does not establish that either proposition is false.
// In particular, different models or experimental domains may both be valid.
func (s *Store) RecordPropositionDispute(leftID, rightID, reason string) (Value, error) {
	return s.with(func(db *database) Value {
		insist(leftID != rightID, "a proposition cannot dispute itself")
		reason = mustString(reason, "dispute reason")
		ensureRow(getRow(db, "SELECT id FROM propositions WHERE id=?", leftID), "proposition", leftID)
		ensureRow(getRow(db, "SELECT id FROM propositions WHERE id=?", rightID), "proposition", rightID)
		if strings.Compare(leftID, rightID) > 0 {
			leftID, rightID = rightID, leftID
		}
		previous := getRow(db, "SELECT * FROM proposition_disputes WHERE left_id=? AND right_id=?", leftID, rightID)
		if previous != nil {
			return Value(previous)
		}
		id := uuid()
		exec(db, "INSERT INTO proposition_disputes VALUES(?,?,?,?,'unresolved','',?)", id, leftID, rightID, reason, now())
		return Value(ensureRow(getRow(db, "SELECT * FROM proposition_disputes WHERE id=?", id), "dispute", id))
	})
}
func (s *Store) ResolvePropositionDispute(id, status, explanation string) (Value, error) {
	return s.with(func(db *database) Value {
		insist(valid(status, "unresolved", "different_contexts", "left_supported", "right_supported", "both_unsupported"), "invalid dispute resolution")
		explanation = mustString(explanation, "resolution explanation")
		ensureRow(getRow(db, "SELECT id FROM proposition_disputes WHERE id=?", id), "dispute", id)
		exec(db, "UPDATE proposition_disputes SET status=?,resolution=? WHERE id=?", status, explanation, id)
		result := Value(ensureRow(getRow(db, "SELECT * FROM proposition_disputes WHERE id=?", id), "dispute", id))
		result["mastery_unchanged"] = true
		return result
	})
}
