#!/usr/bin/env python3
"""Rank evaluated idea candidates by a transparent, pool-relative heuristic."""

import argparse
import json
import math
import sys
from collections import Counter
from pathlib import Path
from typing import Any

COMPONENTS = ("coherence", "distinctiveness", "tractability")


def validate_candidates(payload: Any) -> list[dict[str, Any]]:
    if not isinstance(payload, dict) or not isinstance(payload.get("candidates"), list):
        raise ValueError("Input must be a JSON object containing a candidates array")
    items = payload["candidates"]
    if not items:
        raise ValueError("candidates must contain at least one item")

    seen = set()
    for index, item in enumerate(items):
        location = f"candidates[{index}]"
        if not isinstance(item, dict):
            raise ValueError(f"{location} must be an object")
        identifier = item.get("id")
        if not isinstance(identifier, str) or not identifier.strip():
            raise ValueError(f"{location}.id must be a non-empty string")
        if identifier in seen:
            raise ValueError(f"Duplicate candidate id: {identifier}")
        seen.add(identifier)
        if not isinstance(item.get("title"), str) or not item["title"].strip():
            raise ValueError(f"{location}.title must be a non-empty string")
        if "family" in item and (not isinstance(item["family"], str) or not item["family"].strip()):
            raise ValueError(f"{location}.family must be a non-empty string when present")
        for component in COMPONENTS:
            value = item.get(component)
            if isinstance(value, bool) or not isinstance(value, (int, float)):
                raise ValueError(f"{location}.{component} must be a number between 0 and 5")
            if not math.isfinite(value) or not 0 <= value <= 5:
                raise ValueError(f"{location}.{component} must be a finite number between 0 and 5")
    return items


def _z_scores(values: list[float]) -> list[float]:
    if not values:
        return []
    mean = sum(values) / len(values)
    variance = sum((value - mean) ** 2 for value in values) / len(values)
    if variance <= 0:
        return [0.0] * len(values)
    scale = math.sqrt(variance)
    return [(value - mean) / scale for value in values]


def rank_candidates(
    candidates: list[dict[str, Any]],
    *,
    beta: float = 0.65,
    feasibility_weight: float = 0.20,
    top: int | None = None,
    max_per_family: int | None = None,
    min_coherence: float | None = None,
    min_tractability: float | None = None,
) -> dict[str, Any]:
    if not 0 <= beta <= 1 or not math.isfinite(beta):
        raise ValueError("beta must be between 0 and 1")
    if not 0 <= feasibility_weight <= 1 or not math.isfinite(feasibility_weight):
        raise ValueError("feasibility_weight must be between 0 and 1")
    if top is not None and top < 1:
        raise ValueError("top must be positive")
    if max_per_family is not None and max_per_family < 1:
        raise ValueError("max_per_family must be positive")
    for label, value in (("min_coherence", min_coherence), ("min_tractability", min_tractability)):
        if value is not None and (not math.isfinite(value) or not 0 <= value <= 5):
            raise ValueError(f"{label} must be between 0 and 5")

    eligible = []
    excluded = []
    for candidate in candidates:
        reasons = []
        if min_coherence is not None and candidate["coherence"] < min_coherence:
            reasons.append(f"coherence below {min_coherence:g}")
        if min_tractability is not None and candidate["tractability"] < min_tractability:
            reasons.append(f"tractability below {min_tractability:g}")
        if reasons:
            excluded.append({"candidate": dict(candidate), "reasons": reasons})
        else:
            eligible.append(candidate)

    z_components = {
        component: _z_scores([float(item[component]) for item in eligible])
        for component in COMPONENTS
    }
    ranked = []
    for index, candidate in enumerate(eligible):
        zc = z_components["coherence"][index]
        zd = z_components["distinctiveness"][index]
        zt = z_components["tractability"][index]
        score = ((1 - feasibility_weight) * ((1 - beta) * zc + beta * zd)
                 + feasibility_weight * zt)
        ranked.append({
            **candidate,
            "fusion_score": score,
            "z_scores": {component: z_components[component][index] for component in COMPONENTS},
        })

    ranked.sort(key=lambda item: (
        -item["fusion_score"], -item["coherence"],
        -item["tractability"], item["id"],
    ))
    for rank, item in enumerate(ranked, start=1):
        item["rank"] = rank

    selected = []
    selected_per_family: Counter[str] = Counter()
    for item in ranked:
        # Missing families are distinct, so unrelated ideas are not grouped accidentally.
        family = item.get("family", f"__ungrouped__:{item['id']}")
        if max_per_family is not None and selected_per_family[family] >= max_per_family:
            continue
        selected.append(item)
        selected_per_family[family] += 1
        if top is not None and len(selected) >= top:
            break

    return {
        "method": "pool-relative heuristic, not trained Alien inference",
        "parameters": {
            "beta": beta,
            "feasibility_weight": feasibility_weight,
            "top": top,
            "max_per_family": max_per_family,
            "min_coherence": min_coherence,
            "min_tractability": min_tractability,
        },
        "counts": {
            "input": len(candidates),
            "eligible": len(eligible),
            "selected": len(selected),
            "excluded": len(excluded),
        },
        "selected": selected,
        "ranked": ranked,
        "excluded": excluded,
        "caveat": "Ratings are subjective assessments; scores are relative to this candidate pool and do not establish novelty or success probabilities.",
    }


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("input", type=str, help="Path to candidate JSON, or - for standard input")
    parser.add_argument("--beta", type=float, default=0.65, help="Distinctiveness share of non-tractability weight (0-1)")
    parser.add_argument("--feasibility-weight", type=float, default=0.20, help="Tractability share of score (0-1)")
    parser.add_argument("--top", type=int, help="Number of candidates to select")
    parser.add_argument("--max-per-family", type=int, help="Maximum selected candidates per family")
    parser.add_argument("--min-coherence", type=float, help="Optional minimum coherence score (0-5)")
    parser.add_argument("--min-tractability", type=float, help="Optional minimum tractability score (0-5)")
    parser.add_argument("--output", type=Path, help="Output JSON file; defaults to standard output")
    return parser


def main() -> None:
    parser = _parser()
    args = parser.parse_args()
    try:
        raw = sys.stdin.read() if args.input == "-" else Path(args.input).read_text(encoding="utf-8")
        candidates = validate_candidates(json.loads(raw))
        result = rank_candidates(
            candidates,
            beta=args.beta,
            feasibility_weight=args.feasibility_weight,
            top=args.top,
            max_per_family=args.max_per_family,
            min_coherence=args.min_coherence,
            min_tractability=args.min_tractability,
        )
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        parser.error(str(exc))
    text = json.dumps(result, indent=2, ensure_ascii=False, allow_nan=False) + "\n"
    if args.output is None:
        sys.stdout.write(text)
    else:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(text, encoding="utf-8")


if __name__ == "__main__":
    main()
