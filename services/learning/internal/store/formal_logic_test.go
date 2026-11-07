package store

import (
	"strings"
	"testing"
)

func checkFormal(t *testing.T, premises []string, conclusion string, atoms map[string]string) Value {
	t.Helper()
	result, err := checkPropositional(premises, conclusion, atoms)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestPropositionalValidityAndCountermodels(t *testing.T) {
	atoms := map[string]string{"P": "Switch closed", "Q": "Lamp lit"}
	valid := checkFormal(t, []string{"P -> Q", "P"}, "Q", atoms)
	if valid["verdict"] != "valid_on_formalization" || valid["counterexample"] != nil ||
		len(valid["redundant_premise_indexes"].([]int)) != 0 || valid["assignments_checked"] != 4 {
		t.Fatal("modus ponens must be valid with neither premise redundant", valid)
	}
	invalid := checkFormal(t, []string{"P -> Q", "Q"}, "P", atoms)
	if invalid["verdict"] != "counterexample_found" {
		t.Fatal("affirming the consequent must be rejected", invalid)
	}
	countermodel := invalid["counterexample"].(map[string]bool)
	if countermodel["P"] || !countermodel["Q"] {
		t.Fatal("the countermodel must make all premises true and the conclusion false", countermodel)
	}
	if invalid["translation_verified"] != false || invalid["natural_language_entailment_certified"] != false {
		t.Fatal("a truth table does not establish faithful natural-language translation", invalid)
	}
}

func TestPropositionalInconsistentAndRedundantPremises(t *testing.T) {
	atoms := map[string]string{"P": "Premise", "Q": "Conclusion"}
	inconsistent := checkFormal(t, []string{"P", "!P"}, "Q", atoms)
	if inconsistent["verdict"] != "inconsistent_premises" || inconsistent["consistent_assignment"] != nil {
		t.Fatal("explosion is not pedagogical evidence of a sound inference", inconsistent)
	}
	redundant := checkFormal(t, []string{"P", "P & Q"}, "P", atoms)
	if redundant["verdict"] != "valid_on_formalization" ||
		len(redundant["redundant_premise_indexes"].([]int)) != 2 {
		t.Fatal("both premises can be removed individually, but not simultaneously", redundant)
	}
}

func TestPropositionalGroupingAndImplicationAssociativity(t *testing.T) {
	atoms := map[string]string{"P": "P", "Q": "Q", "R": "R"}
	test := checkFormal(t, []string{"P -> Q -> R", "P", "!R"}, "!Q", atoms)
	if test["verdict"] != "valid_on_formalization" {
		t.Fatal("implication must associate to the right", test)
	}
	grouping := checkFormal(t, []string{"P | Q & R", "!P", "!R"}, "P", atoms)
	if grouping["verdict"] != "inconsistent_premises" {
		t.Fatal("AND must bind more tightly than OR", grouping)
	}
}

func TestPropositionalRejectsUnsupportedAndOversizedInput(t *testing.T) {
	atoms := map[string]string{"P": "Fact", "Q": "Outcome"}
	cases := []struct {
		name       string
		premises   []string
		conclusion string
		meanings   map[string]string
	}{
		{"missing atom", []string{"P"}, "Q", map[string]string{"P": "Fact"}},
		{"unused atom", []string{"P"}, "P", atoms},
		{"unknown operator", []string{"P => Q"}, "Q", atoms},
		{"unbalanced", []string{"(P & Q"}, "P", atoms},
		{"trailing", []string{"P Q"}, "Q", atoms},
		{"invalid lowercase", []string{"p"}, "P", map[string]string{"P": "Fact"}},
		{"unsupported quantifier", []string{"∀P"}, "P", map[string]string{"P": "Fact"}},
		{"too much nesting", []string{strings.Repeat("!", 34) + "P"}, "P", map[string]string{"P": "Fact"}},
		{"too long", []string{strings.Repeat("P | ", 100) + "P"}, "P", map[string]string{"P": "Fact"}},
		{"invalid meaning", []string{"P"}, "P", map[string]string{"P": "  "}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := checkPropositional(tc.premises, tc.conclusion, tc.meanings); err == nil {
				t.Fatal("invalid or ambiguous formalization was accepted")
			}
		})
	}
	tooMany := map[string]string{}
	formulas := []string{}
	for i := 0; i < maxFormalAtoms+1; i++ {
		name := string(rune('A' + i))
		tooMany[name] = name
		formulas = append(formulas, name)
	}
	if _, err := checkPropositional(formulas, "A", tooMany); err == nil {
		t.Fatal("unbounded atom sets must be rejected")
	}
}

func deductiveChainFixture(t *testing.T) (*Store, map[string]string, string) {
	t.Helper()
	s := newExtendedStore(t)
	source, err := s.IngestText("Circuit argument", "notes", "text/plain",
		"If the switch is closed, the lamp is lit. The switch is closed. The lamp is lit.")
	if err != nil {
		t.Fatal(err)
	}
	work, err := s.NextExtraction(source["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	claims := []ExtractionCandidate{
		{Statement: "If the switch is closed, the lamp is lit", Context: "binary circuit", Domain: "engineering", Quote: "If the switch is closed, the lamp is lit"},
		{Statement: "The switch is closed", Context: "binary circuit", Domain: "engineering", Quote: "The switch is closed"},
		{Statement: "The lamp is lit", Context: "binary circuit", Domain: "engineering", Quote: "The lamp is lit"},
	}
	batch, err := s.ProposeExtraction(work["span_id"].(string), ExtractionProposal{
		Claims: claims,
		Chains: []ExtractionChain{{Conclusion: 2, Premises: []int{0, 1}, Relation: "entails", Explanation: "Modus ponens under the stipulated circuit model"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	decisions := []ExtractionDecision{
		{Index: 0, Action: "accept"},
		{Index: 1, Action: "accept"},
		{Index: 2, Action: "accept"},
	}
	if _, err := s.AuditExtraction(batch["batch_id"].(string), decisions, []ChainDecision{{Index: 0, Keep: true}}); err != nil {
		t.Fatal(err)
	}
	graph, err := s.ReasoningGraph(20)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, n := range graph["propositions"].([]Value) {
		ids[n["statement"].(string)] = n["id"].(string)
	}
	chains := graph["chains"].([]Value)
	if len(chains) != 1 {
		t.Fatal("one deductive chain expected", graph)
	}
	return s, ids, chains[0]["id"].(string)
}

func TestReasoningEntailmentStoredAndBoundToExactClaims(t *testing.T) {
	s, ids, chainID := deductiveChainFixture(t)
	form := ReasoningFormalization{
		Premises: map[string]string{
			ids["If the switch is closed, the lamp is lit"]: "P -> Q",
			ids["The switch is closed"]:                     "P",
		},
		Conclusion: "Q",
		Atoms:      map[string]string{"P": "Switch is closed", "Q": "Lamp is lit"},
		Scope:      "Ideal binary circuit with no broken components",
	}
	result, err := s.CheckReasoningEntailment(chainID, form)
	if err != nil {
		t.Fatal(err)
	}
	proof := result["result"].(Value)
	if proof["verdict"] != "valid_on_formalization" || result["claim_snapshot_current"] != true ||
		proof["mastery_unchanged"] != true || proof["natural_language_entailment_certified"] != false {
		t.Fatal("the result must certify only the supplied formulas", result)
	}
	repeated, err := s.CheckReasoningEntailment(chainID, form)
	if err != nil || repeated["duplicate"] != true || repeated["id"] != result["id"] {
		t.Fatalf("retries must be idempotent: result=%v err=%v", repeated, err)
	}
	chain, err := s.GetReasoningChain(chainID)
	if err != nil || len(chain["formal_checks"].([]Value)) != 1 {
		t.Fatalf("chain detail must expose the check: chain=%v err=%v", chain, err)
	}
	graph, err := s.ReasoningGraph(20)
	if err != nil {
		t.Fatal(err)
	}
	latest := graph["chains"].([]Value)[0]["latest_formal_check"].(Value)
	if latest["verdict"] != "valid_on_formalization" || latest["natural_language_entailment_certified"] != false {
		t.Fatal("the graph must not misrepresent the scope of the formal verdict", latest)
	}
	invalid := form
	invalid.Premises = map[string]string{
		ids["If the switch is closed, the lamp is lit"]: "P -> Q",
		ids["The switch is closed"]:                     "Q",
	}
	invalid.Conclusion = "P"
	other, err := s.CheckReasoningEntailment(chainID, invalid)
	if err != nil || other["result"].(Value)["verdict"] != "counterexample_found" {
		t.Fatalf("counterexamples must be saved separately: result=%v err=%v", other, err)
	}
	all, err := s.GetReasoningFormalChecks(chainID, 10)
	if err != nil || len(all["checks"].([]Value)) != 2 {
		t.Fatalf("formal checks must remain inspectable: report=%v err=%v", all, err)
	}
	if _, err := s.with(func(db *database) Value {
		exec(db, "UPDATE propositions SET statement=? WHERE id=?", "The switch was reported closed", ids["The switch is closed"])
		return Value{}
	}); err != nil {
		t.Fatal(err)
	}
	chain, err = s.GetReasoningChain(chainID)
	if err != nil || chain["formal_checks"].([]Value)[0]["claim_snapshot_current"] != false {
		t.Fatal("modified proposition wording must invalidate formalization binding", chain, err)
	}
}

func TestReasoningEntailmentRejectsMissingOrWrongRelations(t *testing.T) {
	s, ids, chainID := deductiveChainFixture(t)
	form := ReasoningFormalization{
		Premises:   map[string]string{ids["The switch is closed"]: "P"},
		Conclusion: "P",
		Atoms:      map[string]string{"P": "Switch is closed"},
		Scope:      "binary circuit",
	}
	if _, err := s.CheckReasoningEntailment(chainID, form); err == nil {
		t.Fatal("a formal check must not omit required source premise IDs")
	}
	if _, err := s.GetReasoningFormalChecks(chainID, 10); err != nil {
		t.Fatal(err)
	}
	supported, nodes, chain := reviewedChainFixture(t)
	form.Premises = map[string]string{}
	for _, v := range []string{"Ohm's law", "Kirchhoff's voltage law"} {
		form.Premises[nodes[v]] = "P"
	}
	if _, err := supported.CheckReasoningEntailment(chain, form); err == nil {
		t.Fatal("empirical/supportive relations must not be certified deductively")
	}
}
