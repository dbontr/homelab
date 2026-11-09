"""Tests for the deterministic ideas candidate ranking helper."""

import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from rank_candidates import _z_scores, rank_candidates, validate_candidates


CANDIDATES = [
    {"id": "a", "title": "Coherent", "family": "one", "coherence": 5, "distinctiveness": 1, "tractability": 5},
    {"id": "b", "title": "Distinctive", "family": "two", "coherence": 3, "distinctiveness": 5, "tractability": 2},
    {"id": "c", "title": "Balanced", "family": "two", "coherence": 4, "distinctiveness": 3, "tractability": 4},
]


class CandidateValidationTests(unittest.TestCase):
    def test_valid_candidates(self):
        self.assertEqual(len(validate_candidates({"candidates": CANDIDATES})), 3)

    def test_reject_duplicate_identifiers(self):
        with self.assertRaisesRegex(ValueError, "Duplicate"):
            validate_candidates({"candidates": [CANDIDATES[0], CANDIDATES[0]]})

    def test_reject_nonfinite_scores(self):
        bad = dict(CANDIDATES[0], coherence=float("nan"))
        with self.assertRaisesRegex(ValueError, "finite"):
            validate_candidates({"candidates": [bad]})

    def test_reject_boolean_scores(self):
        bad = dict(CANDIDATES[0], coherence=True)
        with self.assertRaisesRegex(ValueError, "number"):
            validate_candidates({"candidates": [bad]})

    def test_reject_empty_family(self):
        bad = dict(CANDIDATES[0], family="")
        with self.assertRaisesRegex(ValueError, "family"):
            validate_candidates({"candidates": [bad]})


class RankingTests(unittest.TestCase):
    def test_coherence_and_distinctiveness_tradeoff(self):
        a = rank_candidates(CANDIDATES, beta=0.0, feasibility_weight=0.0)
        b = rank_candidates(CANDIDATES, beta=1.0, feasibility_weight=0.0)
        self.assertEqual(a["ranked"][0]["id"], "a")
        self.assertEqual(b["ranked"][0]["id"], "b")

    def test_constant_column_is_zero(self):
        self.assertEqual(_z_scores([2, 2, 2]), [0.0, 0.0, 0.0])

    def test_deterministic_ties(self):
        tied = [
            {"id": ident, "title": ident, "coherence": 3, "distinctiveness": 3, "tractability": 3}
            for ident in ["z", "a"]
        ]
        self.assertEqual([x["id"] for x in rank_candidates(tied)["ranked"]], ["a", "z"])

    def test_diversity_limit(self):
        result = rank_candidates(CANDIDATES, max_per_family=1)
        self.assertEqual(len(result["selected"]), 2)
        self.assertEqual({x["family"] for x in result["selected"]}, {"one", "two"})

    def test_exclusions_are_explicit(self):
        result = rank_candidates(CANDIDATES, min_tractability=4)
        self.assertEqual(result["counts"]["excluded"], 1)
        self.assertEqual(result["excluded"][0]["candidate"]["id"], "b")

    def test_one_candidate_has_finite_score(self):
        result = rank_candidates(CANDIDATES[:1])
        self.assertEqual(result["ranked"][0]["fusion_score"], 0.0)

    def test_command_line_output(self):
        with tempfile.TemporaryDirectory() as folder:
            src = Path(folder) / "candidates.json"
            dst = Path(folder) / "ranked.json"
            src.write_text(json.dumps({"candidates": CANDIDATES}), encoding="utf-8")
            run = subprocess.run(
                [sys.executable, str(Path(__file__).with_name("rank_candidates.py")), str(src), "--top", "2", "--output", str(dst)],
                capture_output=True, text=True, check=False,
            )
            self.assertEqual(run.returncode, 0, run.stderr)
            self.assertEqual(len(json.loads(dst.read_text(encoding="utf-8"))["selected"]), 2)


if __name__ == "__main__":
    unittest.main()
