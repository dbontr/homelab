# Ideation protocol

This guide supports deeper investigations when a short idea list would be inadequate.

## Represent a research atom

An atom is a transferable unit of a method, model, observation, constraint, or proof strategy. Prefer verb-and-object descriptions:

| Weak label | Useful atom |
| --- | --- |
| Machine learning | Use contrastive representations to distinguish structurally similar systems |
| FPGA | Update many local state variables concurrently with bounded interconnect |
| Statistical physics | Minimize an energy function whose local terms encode global constraints |
| Formal verification | Translate a proposed invariant into a decidable or proof-assistant-checkable obligation |
| User research | Measure real task completion before estimating product demand |

Maintain a small provenance table: `atom_id`, `statement`, `type`, `source`, and `confidence`. A source may be a publication, repository, supplied artifact, or an explicitly tagged inference. Never invent a paper or a citation to justify an atom.

## Generate candidates by transformation

1. **Mechanism transfer:** Move an established mechanism into a setting where its assumptions can still hold, e.g. a sparse local optimization rule in reconfigurable hardware.
2. **Bottleneck exchange:** Replace a resource-heavy stage with a different physical or mathematical resource; calculate what is traded away.
3. **Constraint inversion:** Deliberately reverse an assumption; ask whether the original proof or algorithm survives.
4. **Dual representation:** Search for an equivalent algebraic, graph, geometric, probabilistic, or physical description that enables a previously unavailable operation.
5. **Scale or regime shift:** Apply a mechanism in a domain where asymptotic behavior, noise, density, or resource limits change the conclusion.
6. **Boundary intersection:** Combine methods whose prerequisites overlap even when their literatures are disjoint.
7. **Negative-space search:** Identify standard combinations conspicuously absent from the literature, then search for plausible reasons before claiming an opportunity.

For each candidate write a one-sentence causal hypothesis: "If [mechanism] operates under [conditions], then [observable result] should differ from [baseline] because [reason]."

Reject combinations supported only by shared vocabulary or metaphor. Prefer a defensible small innovation to an extravagant untestable synthesis.

## Novelty and prior-art audit

- Search the exact mechanism, close terminological variants, neighboring terms, and the combination of its ingredients. Consult references cited by the closest work and subsequent work citing it when possible.
- For software and engineering, inspect existing open-source implementations and patents where relevant. For mathematical conjectures, examine known lemmas, counterexamples, and the scope of adjacent theorems.
- Report the closest inspected precedents and the precise difference; allow the conclusion "already known" or "cannot establish novelty".
- Record a bounded search statement: which databases or tools, approximate date, key terms, and any gaps. Absence of results is weaker evidence than a constructive claim that a mechanism differs from a known baseline.
- Do not fabricate performance claims, research outcomes, patentability, or scholarly priority.

## Critical tests by discipline

- **Mathematics:** state assumptions and quantifiers; reduce to a toy case; seek counterexamples; identify exact lemma/proof gap; use formal tools only where available.
- **Algorithms/AI:** compare to a strong simple baseline; articulate computational complexity; check data contamination, leakage, and realistic deployment constraints.
- **Physics:** list conserved quantities, dimensional checks, limiting cases, noise sources, and observables; separate logical models from realizable apparatus.
- **Hardware:** include bandwidth, memory, power, I/O, timing closure, and interface assumptions; estimate the bottleneck before claiming acceleration.
- **Product/system concepts:** test who experiences the problem, what job is completed, why incumbent alternatives fail, and how a cheap prototype could invalidate the demand assumption.

## Portfolio construction

A useful portfolio has distinct mechanisms, not merely different names. Explicitly show the tradeoff between novelty, explanatory coherence, and cheap verification. Do not optimize a single numeric score at the expense of decisive weaknesses. For each shortlisted idea provide its minimum viable experiment or proof attempt, expected contrary result, and stop/pivot rule. If tools cannot inspect current prior art, make novelty provisional and emphasize falsifiable questions.
