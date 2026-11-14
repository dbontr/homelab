# Candidate comparison and ranking

`rank_candidates.go` ranks already evaluated ideas. It does not generate candidates, determine research novelty, or run the Alien Space of Science models. It uses only Go's standard library.

## Input

Pass a JSON object with a `candidates` array. Every item needs a unique string `id`, a non-empty `title`, and three finite scores in the inclusive range 0-5: `coherence`, `distinctiveness`, and `tractability`. Other candidate fields, such as `family`, `evidence_status`, `mechanism`, `source_links`, or `test`, are preserved.

```json
{
  "candidates": [
    {"id": "a", "title": "Idea A", "family": "hardware", "coherence": 4, "distinctiveness": 5, "tractability": 2, "evidence_status": "unchecked"},
    {"id": "b", "title": "Idea B", "family": "methods", "coherence": 5, "distinctiveness": 2, "tractability": 5, "evidence_status": "related"},
    {"id": "c", "title": "Idea C", "family": "methods", "coherence": 3, "distinctiveness": 4, "tractability": 4, "evidence_status": "no-close-match-found"}
  ]
}
```

Score anchors:

| Score | Coherence | Distinctiveness | Tractability |
| --- | --- | --- | --- |
| 0 | Mechanism contradicts known constraints | Same as known baseline | No clear test |
| 1 | Critical unexplained link | Cosmetic variant | Impractical with available resources |
| 3 | Plausible but dependent on an uncertain assumption | Uncommon pairing with nearby precedent | Achievable focused test with effort |
| 5 | Precise mechanism and supported prerequisites | No close match in a bounded, documented search | Cheap, discriminating experiment or proof check |

Intermediate values reflect judgment. Even `5` distinctiveness is *not* a claim of priority or patentable novelty. For unchecked work, do not award the maximum distinctiveness without identifying that the score is speculative.

## Ranking formula

For every candidate pool, use population z-scores `z(x) = (x - mean(x)) / population_sd(x)`. A constant column becomes zero. The script calculates:

```text
score = (1 - feasibility_weight) * (
    (1 - beta) * z(coherence) + beta * z(distinctiveness)
) + feasibility_weight * z(tractability)
```

Defaults: `beta=0.65`, `feasibility_weight=0.20`. Both are within `[0,1]`. Scores are relative to the supplied pool, so do not compare scores produced on different pools. A small pool or correlated ratings can make the result unstable. Never interpret the score as a calibrated probability of scientific success.

## Usage

```bash
go run ideas/scripts/rank_candidates.go candidates.json --top 5
go run ideas/scripts/rank_candidates.go candidates.json --beta 0.4 --feasibility-weight 0.35 --max-per-family 2 --output ranked.json
go test ideas/scripts/rank_candidates.go ideas/scripts/rank_candidates_test.go
```

`--min-coherence` and `--min-tractability` optionally filter weak proposals. Without these flags, all candidates remain eligible. `--max-per-family N` restricts selected candidates to at most N from a single explicitly supplied `family`; missing family values are treated as distinct. If `--top` is omitted, all eligible candidates are selected. Output includes the selected candidates, full ranking, component z-scores, weights, and the original candidate fields. Report any filtering or diversity limits alongside a shortlist.
