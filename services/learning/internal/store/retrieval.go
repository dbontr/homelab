package store

import (
	"strings"
	"unicode"
)

// SearchKnowledge uses the local SQLite FTS5 index. It ranks matching source
// passages; it is lexical retrieval, not an embedding-based semantic engine.
func (s *Store) SearchKnowledge(query string, limit int) (Value, error) {
	return s.with(func(db *database) Value {
		insist(limit >= 1 && limit <= 50, "limit must be 1..50")
		insist(len(query) <= 250, "query is too long")
		tokens := []string{}
		current := []rune{}
		flush := func() {
			if len(current) > 0 {
				tokens = append(tokens, string(current))
				current = nil
			}
		}
		for _, r := range query {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				current = append(current, r)
			} else {
				flush()
			}
		}
		flush()
		insist(len(tokens) > 0 && len(tokens) <= 20, "search query must contain 1..20 words")
		terms := make([]string, 0, len(tokens))
		for _, token := range tokens {
			terms = append(terms, `"`+token+`"`)
		}
		match := strings.Join(terms, " OR ")
		matches := mapRows(getRows(db, `SELECT source_search.span_id,source_search.source_id,sp.ordinal,sp.content,ks.label AS source_name,bm25(source_search) AS rank
            FROM source_search JOIN source_spans sp ON sp.id=source_search.span_id
            JOIN knowledge_sources ks ON ks.id=source_search.source_id
            WHERE source_search MATCH ? ORDER BY rank LIMIT ?`, match, limit))
		return Value{"matches": matches, "query": query, "retrieval": "local_fts5", "model_based_ranking": false}
	})
}
