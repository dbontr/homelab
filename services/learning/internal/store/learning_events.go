package store

import (
	"fmt"
	"strings"
)

// learningEventSpan preserves the role, immutable message origin and authored text.
func learningEventSpan(role, originID, content string) string {
	return fmt.Sprintf("[%s · %s]\n%s", role, originID, content)
}

func (s *Store) AppendLearningEvent(sessionID, originID, role, content string) (Value, error) {
	return s.with(func(db *database) Value {
		ensureRow(getRow(db, "SELECT id FROM learning_sessions WHERE id=?", sessionID), "learning session", sessionID)
		originID = mustString(originID, "origin_id")
		insist(len(originID) <= 200, "origin_id too long")
		insist(valid(role, "learner", "tutor", "tool", "source"), "invalid learning event role")
		content = mustString(content, "content")
		previous := getRow(db, "SELECT id,role,content FROM learning_events WHERE session_id=? AND origin_id=?", sessionID, originID)
		if previous != nil {
			insist(strVal(previous["role"]) == role && strVal(previous["content"]) == content, "origin_id was used for a different event")
			return Value{"event_id": previous["id"], "duplicate": true}
		}
		eventID := uuid()
		exec(db, "INSERT INTO learning_events VALUES(?,?,?,?,?,?)", eventID, sessionID, originID, role, content, now())
		sourceHash := fingerprint("learning-session", sessionID)
		source := getRow(db, "SELECT id FROM knowledge_sources WHERE sha256=? AND kind='conversation'", sourceHash)
		sourceID := ""
		if source == nil {
			sourceID = uuid()
			exec(db, "INSERT INTO knowledge_sources VALUES(?,?,?,?,?,?,?)", sourceID, "Learning session "+sessionID, "conversation", "text/markdown", "", sourceHash, now())
		} else {
			sourceID = strVal(source["id"])
		}
		next := countOrPanic(db, "SELECT COUNT(*) FROM source_spans WHERE source_id=?", sourceID)
		// Preserve exact role, source event and author-text for extraction audit.
		recorded := learningEventSpan(role, originID, content)
		exec(db, "INSERT INTO source_spans(id,source_id,ordinal,content,fingerprint) VALUES(?,?,?,?,?)", uuid(), sourceID, next, recorded, fingerprint(recorded))
		return Value{"event_id": eventID, "source_id": sourceID, "duplicate": false, "source_coverage_pending": true}
	})
}
func (s *Store) LearningEvents(sessionID string, limit int) (Value, error) {
	return s.with(func(db *database) Value {
		ensureRow(getRow(db, "SELECT 1 FROM learning_sessions WHERE id=?", sessionID), "learning session", sessionID)
		insist(limit >= 1 && limit <= 100, "limit must be 1..100")
		events := mapRows(getRows(db, "SELECT id,origin_id,role,content,recorded_at FROM learning_events WHERE session_id=? ORDER BY rowid DESC LIMIT ?", sessionID, limit))
		// The response is chronological despite the most-recent-first query.
		for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
			events[i], events[j] = events[j], events[i]
		}
		sourceHash := fingerprint("learning-session", sessionID)
		source := getRow(db, "SELECT id FROM knowledge_sources WHERE sha256=? AND kind='conversation'", sourceHash)
		sourceID := ""
		if source != nil {
			sourceID = strVal(source["id"])
		}
		return Value{"events": events, "source_id": sourceID, "session_id": sessionID}
	})
}
func (s *Store) SearchSources(term string, limit int) (Value, error) {
	return s.with(func(db *database) Value {
		term = strings.TrimSpace(term)
		insist(len(term) >= 2 && len(term) <= 120, "search term must be 2..120 characters")
		insist(limit >= 1 && limit <= 50, "limit must be 1..50")
		// Escape LIKE metacharacters: user input is a literal substring.
		term = strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(term)
		rows := getRows(db, `SELECT sp.source_id,sp.ordinal,sp.content,ks.label AS source_name FROM source_spans sp JOIN knowledge_sources ks ON ks.id=sp.source_id
          WHERE sp.content LIKE ? ESCAPE '\' ORDER BY ks.created_at DESC,sp.ordinal LIMIT ?`, "%"+term+"%", limit)
		return Value{"matches": mapRows(rows)}
	})
}
