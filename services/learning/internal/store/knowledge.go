package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// ExtractionCandidate is a source-grounded proposition proposed by the host model.
// Candidates are not promoted to established facts or learner ability by ingestion.
type ExtractionCandidate struct {
	Statement string `json:"statement"`
	Context   string `json:"context"`
	Domain    string `json:"domain"`
	Quote     string `json:"quote"`
}

type ExtractionChain struct {
	Conclusion  int    `json:"conclusion"`
	Premises    []int  `json:"premises"`
	Relation    string `json:"relation"`
	Explanation string `json:"explanation"`
}

type ExtractionProposal struct {
	Claims []ExtractionCandidate `json:"claims"`
	Chains []ExtractionChain     `json:"chains"`
}

type ExtractionDecision struct {
	Index   int    `json:"index"`
	Action  string `json:"action"` // accept, reuse, reject
	ReuseID string `json:"reuse_id,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

type ChainDecision struct {
	Index int  `json:"index"`
	Keep  bool `json:"keep"`
}

func fingerprint(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte(fmt.Sprintf("%d:", len(part))))
		_, _ = h.Write([]byte(part))
	}
	return hex.EncodeToString(h.Sum(nil))
}
func canonical(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(s)), " "))
}
func identityHash(statement, context string) string {
	return fingerprint(canonical(statement), canonical(context))
}

func sourceChunks(text string) []string {
	// Rune slicing avoids breaking UTF-8 inside mathematical notation or non-Latin sources.
	runes := []rune(text)
	chunks := []string{}
	const max = 4000
	for len(runes) > 0 {
		take := len(runes)
		if take > max {
			take = max
			for i := max; i >= 2600; i-- {
				if runes[i-1] == '\n' || runes[i-1] == ' ' {
					take = i
					break
				}
			}
		}
		segment := strings.TrimSpace(string(runes[:take]))
		if segment != "" {
			chunks = append(chunks, segment)
		}
		runes = runes[take:]
	}
	return chunks
}

func sourcePublic(db *database, id string) Value {
	r := ensureRow(getRow(db, "SELECT id,label,kind,mime_type,sha256,created_at FROM knowledge_sources WHERE id=?", id), "source", id)
	out := Value(r)
	counts := getRow(db, `SELECT COUNT(*) AS total, SUM(CASE WHEN processed_at IS NOT NULL THEN 1 ELSE 0 END) AS covered FROM source_spans WHERE source_id=?`, id)
	out["spans"] = intVal(counts["total"])
	out["covered_spans"] = intVal(counts["covered"])
	return out
}

// sourceSpanIntact binds the stored span and its position to the unchanged
// source text. Its hashes detect accidental mutation, not a malicious writer
// with access to the database.
func sourceSpanIntact(db *database, span row) bool {
	if fingerprint(strVal(span["content"])) != strVal(span["fingerprint"]) {
		return false
	}
	source := getRow(db, "SELECT label,kind,content,sha256 FROM knowledge_sources WHERE id=?", span["source_id"])
	if source == nil {
		return false
	}
	if strVal(source["kind"]) == "conversation" && strVal(source["content"]) == "" {
		// Learning-session transcripts are append-only event sources. Their
		// source digest identifies the session, not an immutable text blob.
		const prefix = "Learning session "
		label := strVal(source["label"])
		if !strings.HasPrefix(label, prefix) {
			return false
		}
		sessionID := strings.TrimPrefix(label, prefix)
		if sessionID == "" || fingerprint("learning-session", sessionID) != strVal(source["sha256"]) {
			return false
		}
		event := getRow(db, "SELECT role,origin_id,content FROM learning_events WHERE session_id=? ORDER BY rowid LIMIT 1 OFFSET ?", sessionID, span["ordinal"])
		return event != nil && learningEventSpan(strVal(event["role"]), strVal(event["origin_id"]), strVal(event["content"])) == strVal(span["content"])
	}
	if fingerprint(strVal(source["kind"]), strVal(source["content"])) != strVal(source["sha256"]) {
		return false
	}
	parts := sourceChunks(strVal(source["content"]))
	index := intVal(span["ordinal"])
	return index >= 0 && index < len(parts) && parts[index] == strVal(span["content"])
}

// IngestText stores the exact submitted text with deterministic source de-duplication.
// PDF and image extraction must be carried out by a model or external parser; this
// method never claims that an opaque binary contains verified prose.
func (s *Store) IngestText(label, kind, mimeType, text string) (Value, error) {
	return s.with(func(db *database) Value {
		label = mustString(label, "label")
		insist(valid(kind, "document", "conversation", "notes", "web", "observation"), "invalid source kind")
		insist(valid(mimeType, "text/plain", "text/markdown"), "source text must be plain text or Markdown")
		insist(utf8.ValidString(text) && len(text) > 0 && len(text) <= 2<<20, "source text must be valid UTF-8 and at most 2 MiB")
		insist(strings.TrimSpace(text) != "", "source text must not be blank")
		sha := fingerprint(kind, text)
		if prior := getRow(db, "SELECT id FROM knowledge_sources WHERE sha256=? AND kind=?", sha, kind); prior != nil {
			out := sourcePublic(db, strVal(prior["id"]))
			out["duplicate"] = true
			return out
		}
		id := uuid()
		exec(db, "INSERT INTO knowledge_sources VALUES(?,?,?,?,?,?,?)", id, label, kind, mimeType, text, sha, now())
		for index, chunk := range sourceChunks(text) {
			exec(db, "INSERT INTO source_spans(id,source_id,ordinal,content,fingerprint) VALUES(?,?,?,?,?)", uuid(), id, index, chunk, fingerprint(chunk))
		}
		out := sourcePublic(db, id)
		out["duplicate"] = false
		return out
	})
}
func (s *Store) ListSources(limit int) (Value, error) {
	return s.with(func(db *database) Value {
		insist(limit > 0 && limit <= 100, "limit must be 1..100")
		out := []Value{}
		for _, r := range getRows(db, "SELECT id FROM knowledge_sources ORDER BY created_at DESC,id DESC LIMIT ?", limit) {
			out = append(out, sourcePublic(db, strVal(r["id"])))
		}
		return Value{"sources": out}
	})
}

func extractionPublic(db *database, id string) Value {
	batch := ensureRow(getRow(db, "SELECT * FROM extraction_batches WHERE id=?", id), "extraction batch", id)
	span := ensureRow(getRow(db, "SELECT * FROM source_spans WHERE id=?", strVal(batch["span_id"])), "source span", strVal(batch["span_id"]))
	return Value{"batch_id": id, "status": batch["status"], "proposals_json": batch["proposals_json"], "source_id": span["source_id"], "span_id": span["id"], "ordinal": span["ordinal"], "source_text": span["content"]}
}

// NextExtraction returns either a pending audit, a fresh source chunk, or completion.
// One pending audit is returned before opening another model extraction batch.
func (s *Store) NextExtraction(sourceID string) (Value, error) {
	return s.with(func(db *database) Value {
		ensureRow(getRow(db, "SELECT id FROM knowledge_sources WHERE id=?", sourceID), "source", sourceID)
		pending := getRow(db, `SELECT b.id FROM extraction_batches b JOIN source_spans sp ON sp.id=b.span_id WHERE sp.source_id=? AND b.status='pending_audit' ORDER BY sp.ordinal LIMIT 1`, sourceID)
		if pending != nil {
			out := extractionPublic(db, strVal(pending["id"]))
			out["action"] = "audit"
			return out
		}
		span := getRow(db, "SELECT * FROM source_spans WHERE source_id=? AND processed_at IS NULL ORDER BY ordinal LIMIT 1", sourceID)
		if span == nil {
			return Value{"action": "complete", "source": sourcePublic(db, sourceID)}
		}
		insist(sourceSpanIntact(db, span), "source integrity mismatch before extraction")
		return Value{"action": "extract", "source_id": sourceID, "span_id": span["id"], "ordinal": span["ordinal"], "content": span["content"], "fingerprint": span["fingerprint"]}
	})
}
func (s *Store) ProposeExtraction(spanID string, proposal ExtractionProposal) (Value, error) {
	return s.with(func(db *database) Value {
		span := ensureRow(getRow(db, "SELECT * FROM source_spans WHERE id=?", spanID), "source span", spanID)
		insist(sourceSpanIntact(db, span), "source integrity mismatch before proposal")
		insist(len(proposal.Claims) > 0 && len(proposal.Claims) <= 32, "provide 1..32 claims")
		insist(len(proposal.Chains) <= 64, "at most 64 reasoning chains per batch")
		for _, candidate := range proposal.Claims {
			insist(strings.TrimSpace(candidate.Statement) != "" && len(candidate.Statement) <= 4000, "claim statement must be 1..4000 characters")
			insist(len(candidate.Context) <= 2000 && len(candidate.Domain) <= 200, "claim context or domain too long")
			quote := strings.TrimSpace(candidate.Quote)
			insist(quote != "" && len(quote) <= 4000 && strings.Contains(strVal(span["content"]), quote), "quoted evidence must appear verbatim in the source span")
		}
		for _, ch := range proposal.Chains {
			insist(ch.Conclusion >= 0 && ch.Conclusion < len(proposal.Claims), "invalid conclusion index")
			insist(len(ch.Premises) > 0 && len(ch.Premises) <= 16, "chain needs 1..16 premises")
			insist(valid(ch.Relation, "entails", "supports", "defines", "models"), "invalid chain relation")
			insist(strings.TrimSpace(ch.Explanation) != "" && len(ch.Explanation) <= 4000, "chain explanation required")
			seen := map[int]bool{}
			for _, idx := range ch.Premises {
				insist(idx >= 0 && idx < len(proposal.Claims) && idx != ch.Conclusion && !seen[idx], "invalid or duplicate premise index")
				seen[idx] = true
			}
		}
		prior := getRow(db, "SELECT id FROM extraction_batches WHERE span_id=? AND fingerprint=?", spanID, span["fingerprint"])
		if prior != nil {
			id := strVal(prior["id"])
			existing := extractionPublic(db, id)
			if existing["status"] == "rejected" {
				exec(db, "UPDATE extraction_batches SET proposals_json=?,status='pending_audit',resolved_at=NULL WHERE id=?", js(proposal), id)
				return extractionPublic(db, id)
			}
			return existing
		}
		insist(getRow(db, "SELECT id FROM extraction_batches WHERE span_id=?", spanID) == nil, "span already has a different extraction batch")
		id := uuid()
		exec(db, "INSERT INTO extraction_batches(id,span_id,fingerprint,proposals_json,status,created_at) VALUES(?,?,?,?,'pending_audit',?)", id, spanID, span["fingerprint"], js(proposal), now())
		return extractionPublic(db, id)
	})
}

// AuditExtraction records a separate source-faithfulness/identity decision.
// No accepted claim, chain or auditor score ever awards learner mastery.
func (s *Store) AuditExtraction(batchID string, decisions []ExtractionDecision, chains []ChainDecision) (Value, error) {
	return s.with(func(db *database) Value {
		batch := ensureRow(getRow(db, "SELECT * FROM extraction_batches WHERE id=?", batchID), "extraction batch", batchID)
		if strVal(batch["status"]) != "pending_audit" {
			return Value{"status": batch["status"], "batch_id": batchID, "duplicate": true}
		}
		spanID := strVal(batch["span_id"])
		span := ensureRow(getRow(db, "SELECT * FROM source_spans WHERE id=?", spanID), "source span", spanID)
		insist(sourceSpanIntact(db, span) && strVal(batch["fingerprint"]) == strVal(span["fingerprint"]), "source integrity mismatch during extraction")
		var proposal ExtractionProposal
		insist(jsonUnmarshal(strVal(batch["proposals_json"]), &proposal) == nil, "invalid stored extraction proposal")
		for _, claim := range proposal.Claims {
			quote := strings.TrimSpace(claim.Quote)
			insist(quote != "" && strings.Contains(strVal(span["content"]), quote), "extraction quotation is absent from its source span")
		}
		insist(len(decisions) == len(proposal.Claims), "all claim candidates must receive an audit decision")
		verdicts := map[int]ExtractionDecision{}
		for _, dec := range decisions {
			insist(dec.Index >= 0 && dec.Index < len(proposal.Claims) && !validDuplicateDecision(verdicts, dec.Index), "invalid or duplicate audit index")
			insist(valid(dec.Action, "accept", "reuse", "reject"), "invalid audit decision")
			if dec.Action == "reuse" {
				insist(dec.ReuseID != "", "reuse requires an existing proposition id")
				ensureRow(getRow(db, "SELECT 1 FROM propositions WHERE id=?", dec.ReuseID), "proposition", dec.ReuseID)
				insist(strings.TrimSpace(dec.Reason) != "", "semantic reuse requires an explicit audit rationale")
			}
			verdicts[dec.Index] = dec
		}
		acceptChains := map[int]bool{}
		for _, choice := range chains {
			insist(choice.Index >= 0 && choice.Index < len(proposal.Chains) && !validDuplicateChain(acceptChains, choice.Index), "invalid or duplicate chain audit index")
			acceptChains[choice.Index] = choice.Keep
		}
		insist(len(chains) == len(proposal.Chains), "all chain candidates must receive an audit decision")
		identities := map[int]string{}
		accepted := 0
		for i, candidate := range proposal.Claims {
			dec := verdicts[i]
			if dec.Action == "reject" {
				continue
			}
			claimID := dec.ReuseID
			hash := identityHash(candidate.Statement, candidate.Context)
			if dec.Action == "accept" {
				if old := getRow(db, "SELECT id FROM propositions WHERE identity_hash=?", hash); old != nil {
					claimID = strVal(old["id"])
				} else {
					claimID = uuid()
					exec(db, "INSERT INTO propositions VALUES(?,?,?,?,?,?,?)", claimID, strings.TrimSpace(candidate.Statement), strings.TrimSpace(candidate.Context), strings.TrimSpace(candidate.Domain), hash, "provisional", now())
				}
			}
			if dec.Action == "reuse" {
				exec(db, "INSERT OR IGNORE INTO proposition_aliases VALUES(?,?,?)", claimID, strings.TrimSpace(candidate.Statement), spanID)
			}
			exec(db, "INSERT OR IGNORE INTO proposition_provenance VALUES(?,?,?,?)", claimID, spanID, strings.TrimSpace(candidate.Quote), batchID)
			identities[i] = claimID
			accepted++
		}
		mergedChains := 0
		for i, ch := range proposal.Chains {
			if !acceptChains[i] {
				continue
			}
			conclusion := identities[ch.Conclusion]
			insist(conclusion != "", "accepted reasoning chain has a rejected conclusion")
			premises := []string{}
			for _, idx := range ch.Premises {
				id := identities[idx]
				insist(id != "", "accepted reasoning chain contains rejected premise")
				insist(id != conclusion, "reasoning chain may not infer a proposition from itself")
				premises = append(premises, id)
			}
			sort.Strings(premises)
			insist(len(set(premises)) == len(premises), "duplicate proposition identity in reasoning chain")
			hash := fingerprint(conclusion, js(premises), ch.Relation)
			chainID := uuid()
			if prior := getRow(db, "SELECT id FROM reasoning_chains WHERE identity_hash=?", hash); prior != nil {
				chainID = strVal(prior["id"])
			} else {
				exec(db, "INSERT INTO reasoning_chains VALUES(?,?,?,?,?,?,?,?)", chainID, conclusion, js(premises), ch.Relation, strings.TrimSpace(ch.Explanation), "provisional", hash, now())
			}
			exec(db, "INSERT OR IGNORE INTO chain_provenance VALUES(?,?,?)", chainID, spanID, batchID)
			mergedChains++
		}
		exec(db, "UPDATE extraction_batches SET status='accepted',resolved_at=? WHERE id=?", now(), batchID)
		exec(db, "INSERT INTO extraction_review_history VALUES(?,?,?,?,?)", uuid(), batchID, "accept", "Source-faithfulness and identity audit accepted", now())
		exec(db, "UPDATE source_spans SET processed_at=? WHERE id=?", now(), spanID)
		return Value{"status": "accepted", "batch_id": batchID, "accepted_claims": accepted, "accepted_chains": mergedChains, "rejected_claims": len(proposal.Claims) - accepted}
	})
}
func validDuplicateDecision(m map[int]ExtractionDecision, index int) bool {
	_, ok := m[index]
	return ok
}
func validDuplicateChain(m map[int]bool, index int) bool { _, ok := m[index]; return ok }

func (s *Store) RejectExtraction(batchID, reason string) (Value, error) {
	return s.with(func(db *database) Value {
		reason = mustString(reason, "reason")
		batch := ensureRow(getRow(db, "SELECT span_id,status FROM extraction_batches WHERE id=?", batchID), "extraction batch", batchID)
		insist(strVal(batch["status"]) == "pending_audit", "only pending batches can be rejected")
		exec(db, "UPDATE extraction_batches SET status='rejected',resolved_at=? WHERE id=?", now(), batchID)
		exec(db, "INSERT INTO extraction_review_history VALUES(?,?,?,?,?)", uuid(), batchID, "reject", reason, now())
		// Keep rejected spans uncovered so a fresh model extraction can be attempted.
		return Value{"batch_id": batchID, "status": "rejected", "reason": reason}
	})
}

func (s *Store) ReasoningGraph(limit int) (Value, error) {
	return s.with(func(db *database) Value {
		insist(limit > 0 && limit <= 500, "limit must be 1..500")
		nodes := []Value{}
		allowed := map[string]bool{}
		for _, r := range getRows(db, "SELECT id,statement,context,domain,status,created_at FROM propositions ORDER BY created_at DESC,id DESC LIMIT ?", limit) {
			id := strVal(r["id"])
			r["evidence_count"] = countOrPanic(db, "SELECT COUNT(*) FROM proposition_provenance WHERE proposition_id=?", id)
			allowed[id] = true
			nodes = append(nodes, Value(r))
		}
		links := []Value{}
		for _, r := range getRows(db, "SELECT * FROM reasoning_chains ORDER BY created_at DESC,id DESC LIMIT ?", limit*4) {
			premises := parseList(r["premises_json"])
			if !allowed[strVal(r["conclusion_id"])] {
				continue
			}
			all := true
			for _, p := range premises {
				if !allowed[p] {
					all = false
				}
			}
			if !all {
				continue
			}
			link := Value{"id": r["id"], "conclusion": r["conclusion_id"], "premises": premises, "relation": r["relation"], "explanation": r["explanation"], "status": r["status"]}
			for k, v := range chainReviewStanding(db, r) {
				link[k] = v
			}
			if check := latestFormalCheck(db, r); check != nil {
				link["latest_formal_check"] = check
			}
			links = append(links, link)
		}
		disputes := []Value{}
		for _, r := range getRows(db, "SELECT * FROM proposition_disputes ORDER BY created_at DESC,id DESC LIMIT ?", limit*4) {
			if allowed[strVal(r["left_id"])] && allowed[strVal(r["right_id"])] {
				disputes = append(disputes, Value(r))
			}
		}
		return Value{"propositions": nodes, "chains": links, "disputes": disputes, "limit": limit, "mastery_separate": true}
	})
}
func (s *Store) GetProposition(id string) (Value, error) {
	return s.with(func(db *database) Value {
		r := ensureRow(getRow(db, "SELECT * FROM propositions WHERE id=?", id), "proposition", id)
		provenance := mapRows(getRows(db, `SELECT k.id AS source_id,sp.id AS source_span_id,sp.ordinal,k.label AS source_name,p.quoted_text,p.batch_id FROM proposition_provenance p JOIN source_spans sp ON sp.id=p.source_span_id JOIN knowledge_sources k ON k.id=sp.source_id WHERE p.proposition_id=? ORDER BY sp.ordinal`, id))
		aliases := mapRows(getRows(db, "SELECT statement FROM proposition_aliases WHERE proposition_id=?", id))
		return Value{"proposition": Value(r), "provenance": provenance, "aliases": aliases, "learner_mastery": nil}
	})
}
func (s *Store) ReviewProposition(id, status, explanation string) (Value, error) {
	return s.with(func(db *database) Value {
		insist(valid(status, "provisional", "reviewed", "disputed"), "invalid proposition status")
		explanation = mustString(explanation, "explanation")
		ensureRow(getRow(db, "SELECT id FROM propositions WHERE id=?", id), "proposition", id)
		exec(db, "UPDATE propositions SET status=? WHERE id=?", status, id)
		return Value{"id": id, "status": status, "reason": explanation, "mastery_unchanged": true}
	})
}
func (s *Store) ReviewReasoningChain(id, status, explanation string) (Value, error) {
	return s.with(func(db *database) Value {
		insist(valid(status, "provisional", "reviewed", "disputed"), "invalid chain status")
		explanation = mustString(explanation, "explanation")
		r := ensureRow(getRow(db, "SELECT * FROM reasoning_chains WHERE id=?", id), "reasoning chain", id)
		if status == "reviewed" {
			blockers := chainReviewBlockers(db, r)
			insist(len(blockers) == 0, "chain review blocked: "+strings.Join(blockers, ", "))
		}
		exec(db, "UPDATE reasoning_chains SET status=? WHERE id=?", status, id)
		exec(db, "INSERT INTO reasoning_chain_review_history VALUES(?,?,?,?,?)", uuid(), id, status, explanation, now())
		r["status"] = status
		out := Value{"id": id, "status": status, "reason": explanation, "mastery_unchanged": true}
		for k, v := range chainReviewStanding(db, r) {
			out[k] = v
		}
		return out
	})
}

func jsonUnmarshal(s string, v any) error { return json.Unmarshal([]byte(s), v) }
