package store

import (
	"fmt"
	"sort"
	"strings"
)

const (
	maxFormalAtoms  = 12
	maxFormulaBytes = 384
	maxFormulaNodes = 128
)

type propositionTerm struct {
	op          byte
	symbol      string
	left, right *propositionTerm
}

func (n *propositionTerm) value(assignment map[string]bool) bool {
	switch n.op {
	case 'a':
		return assignment[n.symbol]
	case '!':
		return !n.left.value(assignment)
	case '&':
		return n.left.value(assignment) && n.right.value(assignment)
	case '|':
		return n.left.value(assignment) || n.right.value(assignment)
	case '>':
		return !n.left.value(assignment) || n.right.value(assignment)
	}
	return false
}

type propositionParser struct {
	text    string
	pos     int
	nodes   int
	depth   int
	symbols map[string]bool
}

func (p *propositionParser) whitespace() {
	for p.pos < len(p.text) {
		switch p.text[p.pos] {
		case ' ', '\n', '\t', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *propositionParser) consume(token string) bool {
	p.whitespace()
	if strings.HasPrefix(p.text[p.pos:], token) {
		p.pos += len(token)
		return true
	}
	return false
}

func (p *propositionParser) node(op byte, left, right *propositionTerm, symbol string) (*propositionTerm, error) {
	p.nodes++
	if p.nodes > maxFormulaNodes {
		return nil, fmt.Errorf("formula exceeds %d operations and atoms", maxFormulaNodes)
	}
	return &propositionTerm{op: op, left: left, right: right, symbol: symbol}, nil
}

func (p *propositionParser) unary() (*propositionTerm, error) {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > 32 {
		return nil, fmt.Errorf("formula nesting exceeds 32 levels")
	}
	if p.consume("!") {
		operand, err := p.unary()
		if err != nil {
			return nil, err
		}
		return p.node('!', operand, nil, "")
	}
	if p.consume("(") {
		expr, err := p.implies()
		if err != nil {
			return nil, err
		}
		if !p.consume(")") {
			return nil, fmt.Errorf("expected closing parenthesis at byte %d", p.pos)
		}
		return expr, nil
	}
	p.whitespace()
	begin := p.pos
	if begin >= len(p.text) || p.text[begin] < 'A' || p.text[begin] > 'Z' {
		return nil, fmt.Errorf("expected uppercase atom at byte %d", begin)
	}
	p.pos++
	for p.pos < len(p.text) {
		c := p.text[p.pos]
		if c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' {
			p.pos++
		} else {
			break
		}
	}
	symbol := p.text[begin:p.pos]
	if len(symbol) > 32 {
		return nil, fmt.Errorf("atom name exceeds 32 characters")
	}
	p.symbols[symbol] = true
	return p.node('a', nil, nil, symbol)
}

func (p *propositionParser) conjunction() (*propositionTerm, error) {
	left, err := p.unary()
	if err != nil {
		return nil, err
	}
	for p.consume("&") {
		right, err := p.unary()
		if err != nil {
			return nil, err
		}
		left, err = p.node('&', left, right, "")
		if err != nil {
			return nil, err
		}
	}
	return left, nil
}

func (p *propositionParser) disjunction() (*propositionTerm, error) {
	left, err := p.conjunction()
	if err != nil {
		return nil, err
	}
	for p.consume("|") {
		right, err := p.conjunction()
		if err != nil {
			return nil, err
		}
		left, err = p.node('|', left, right, "")
		if err != nil {
			return nil, err
		}
	}
	return left, nil
}

func (p *propositionParser) implies() (*propositionTerm, error) {
	left, err := p.disjunction()
	if err != nil {
		return nil, err
	}
	if p.consume("->") {
		right, err := p.implies()
		if err != nil {
			return nil, err
		}
		return p.node('>', left, right, "")
	}
	return left, nil
}

func parseProposition(text string, symbols map[string]bool) (*propositionTerm, error) {
	if len(text) == 0 || len(text) > maxFormulaBytes {
		return nil, fmt.Errorf("formula must contain 1..%d bytes", maxFormulaBytes)
	}
	p := &propositionParser{text: text, symbols: symbols}
	tree, err := p.implies()
	if err != nil {
		return nil, err
	}
	p.whitespace()
	if p.pos != len(text) {
		return nil, fmt.Errorf("unsupported operator or trailing input at byte %d", p.pos)
	}
	return tree, nil
}

func validAtom(symbol string) bool {
	if len(symbol) == 0 || len(symbol) > 32 || symbol[0] < 'A' || symbol[0] > 'Z' {
		return false
	}
	for _, c := range symbol[1:] {
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

func assignmentFor(symbols []string, bits int) map[string]bool {
	values := make(map[string]bool, len(symbols))
	for i, name := range symbols {
		values[name] = bits&(1<<i) != 0
	}
	return values
}

func nullableAssignment(values map[string]bool) any {
	if values == nil {
		return nil
	}
	return values
}

// checkPropositional checks an explicit interpretation, not the natural-language
// equivalence of that interpretation to the original source statements.
func checkPropositional(premiseFormulas []string, conclusionFormula string, meanings map[string]string) (Value, error) {
	if len(premiseFormulas) == 0 || len(premiseFormulas) > 16 {
		return nil, fmt.Errorf("provide 1..16 premise formulas")
	}
	if len(meanings) == 0 || len(meanings) > maxFormalAtoms {
		return nil, fmt.Errorf("provide 1..%d atom meanings", maxFormalAtoms)
	}
	for atom, explanation := range meanings {
		if !validAtom(atom) || strings.TrimSpace(explanation) == "" || len(explanation) > 500 {
			return nil, fmt.Errorf("invalid atom name or empty/oversized meaning")
		}
	}
	used := make(map[string]bool)
	parsed := make([]*propositionTerm, len(premiseFormulas))
	for i, formula := range premiseFormulas {
		tree, err := parseProposition(formula, used)
		if err != nil {
			return nil, fmt.Errorf("premise %d: %w", i+1, err)
		}
		parsed[i] = tree
	}
	conclusion, err := parseProposition(conclusionFormula, used)
	if err != nil {
		return nil, fmt.Errorf("conclusion: %w", err)
	}
	if len(used) != len(meanings) {
		return nil, fmt.Errorf("atom meanings must match the exact set of symbols used")
	}
	symbols := make([]string, 0, len(used))
	for atom := range used {
		if _, ok := meanings[atom]; !ok {
			return nil, fmt.Errorf("missing meaning for atom %s", atom)
		}
		symbols = append(symbols, atom)
	}
	sort.Strings(symbols)
	var witness, counterexample map[string]bool
	necessary := make([]bool, len(parsed))
	assignments := 1 << len(symbols)
	for mask := 0; mask < assignments; mask++ {
		values := assignmentFor(symbols, mask)
		truths := make([]bool, len(parsed))
		allPremises := true
		for i, p := range parsed {
			truths[i] = p.value(values)
			allPremises = allPremises && truths[i]
		}
		conclusionTrue := conclusion.value(values)
		if allPremises {
			if witness == nil {
				witness = values
			}
			if !conclusionTrue && counterexample == nil {
				counterexample = values
			}
		}
		if !conclusionTrue {
			for omitted := range parsed {
				otherPremises := true
				for i, truth := range truths {
					if i != omitted && !truth {
						otherPremises = false
						break
					}
				}
				if otherPremises {
					necessary[omitted] = true
				}
			}
		}
	}
	verdict := "valid_on_formalization"
	if witness == nil {
		verdict = "inconsistent_premises"
	} else if counterexample != nil {
		verdict = "counterexample_found"
	}
	redundant := []int{}
	if verdict == "valid_on_formalization" {
		for i, required := range necessary {
			if !required {
				redundant = append(redundant, i)
			}
		}
	}
	return Value{
		"verdict":                               verdict,
		"logic":                                 "classical_propositional",
		"assignments_checked":                   assignments,
		"consistent_assignment":                 nullableAssignment(witness),
		"counterexample":                        nullableAssignment(counterexample),
		"redundant_premise_indexes":             redundant,
		"translation_verified":                  false,
		"natural_language_entailment_certified": false,
		"mastery_unchanged":                     true,
	}, nil
}
