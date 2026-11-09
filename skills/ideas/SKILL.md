---
name: ideas
description: Generate, investigate, rank, and develop original research, scientific, mathematical, engineering, software, and product ideas. Use when exploring unconventional hypotheses, interdisciplinary discoveries, novel algorithms, thesis or capstone directions, inventions, research agendas, or competing project concepts. Apply idea-atom recombination inspired by the Alien Space of Science. Distinguish genuine evidence from speculative novelty. Turn promising combinations into falsifiable experiments or proof obligations.
---

# Ideas

Search for intellectually coherent directions that are difficult to notice from within one field. Treat ideation as a disciplined process of synthesis, criticism, and experiment design, not merely a list of interesting topics.

## Working contract

- Use the supplied question, papers, notes, repository, datasets, constraints, and available tools. Infer missing preferences when feasible. Ask a question only if it blocks a meaningful result.
- Default to five distinct ideas, a moderate appetite for unconventional combinations, and an achievable first experiment. Preserve all explicit budget, time, equipment, mathematical, and disciplinary constraints.
- Do not silently turn a suggestion into a project, commit, issue, purchase, API call, compute job, or saved record. Take external actions only when requested and authorized.
- Distinguish a conjecture from an established result, an analogy from a mechanism, and a promising combination from a demonstrated discovery.
- The baseline workflow runs with ordinary reasoning and available research tools. It does **not** execute or reproduce the trained Alien models. Use the optional upstream model path only as described in [alien-backend.md](references/alien-backend.md).

## Discovery workflow

1. **Define the search space.** State the problem, desired output (theorem, algorithm, experiment, product, explanation), non-negotiable constraints, and what counts as a meaningful advance. Keep measurable success criteria where possible.
2. **Recover existing knowledge.** Read relevant user materials first. For research-grade novelty, search current primary literature and existing implementations. Prioritize original papers, specifications, documented experiments, and reproducible repositories. Search close synonyms and adjacent disciplines, not only the exact phrasing of the candidate idea. Record what was actually checked.
3. **Extract idea atoms.** Identify small transferable units: mechanisms, structures, observations, proof techniques, optimization objectives, bottlenecks, resource assumptions, and experimental methods. An atom must describe a possible operation or relationship, not merely name a broad topic. Mark each atom as sourced, derived, or speculative.
4. **Construct diverse candidates.** Combine compatible but rarely adjacent atoms. Try mechanism transfer, constraint inversion, duality, symmetry, or costly-step replacement. Also test scale changes and observations that could become computational primitives. Generate more candidates internally than requested. Reject arbitrary word juxtapositions and cosmetic renamings.
5. **Supply a causal account.** For each candidate, describe the mechanism, its justification, the proposed advance, and the assumptions required for it to work. If you cannot state the mechanism, do not promote the idea.
6. **Adversarially evaluate.** Check nearest prior art, impossible assumptions, dimensional or mathematical inconsistencies, known no-go results, missing baselines, experimental confounds, and cheaper conventional approaches. Preserve high-risk ideas when an efficient experiment can test their central uncertainty.
7. **Select a portfolio.** Offer complementary options: at least one tractable near-term proposal, one higher-risk/high-upside proposal, and genuinely different mechanisms. Score only after criticism. Do not let a weighted ranking conceal a fatal flaw or a novelty contradiction.
8. **Make ideas actionable.** Specify the decisive question, simplest proof obligation or experiment, baseline, predicted observation, disconfirming result, resources, estimated difficulty, and a stop/pivot criterion. Prefer tests that can eliminate bad ideas quickly.

For deeper methodological details, disciplinary variants, and adversarial checks, read [ideation-protocol.md](references/ideation-protocol.md).

## Comparison and scoring

Use a **0-5 heuristic rubric** for each candidate when comparison adds value:

- **Coherence (C):** Is there a specific, defensible mechanism linking its components?
- **Distinctiveness (D):** How different is the proposed mechanism from the closest *inspected* work? Distinctiveness is not proof of global novelty.
- **Tractability (T):** Can the user undertake a meaningful experiment, implementation, counterexample search, or proof attempt with the stated resources?

Keep ratings provisional and explain important reasons. When the user wants repeatable ranking across three or more candidates, use `scripts/rank_candidates.py` with the JSON schema and formula in [scoring.md](references/scoring.md). The default blend favors distinctiveness but reserves weight for tractability. Ratings made by reasoning are **not** outputs from a trained cognitive-availability model, nor calibrated probabilities of discovery. Apply evidence checks and hard constraints independently of the ranking.

For a substantial search, track each candidate's evidence status: `existing`, `related`, `no-close-match-found`, or `unchecked`. Never equate `unchecked` with novel. Cite retrieved sources in the final answer, using the available citation mechanism.

## Response patterns

**Exploration:** Start with a compact comparison of the requested number of concepts, showing mechanism, distinctive feature, chief uncertainty, and practical test. Expand the strongest two or three with specific next experiments. Avoid giving every idea the same rhetorical treatment.

**Research proposal:** Present the problem, baseline, atoms, exact mechanism or conjecture, and related work. Identify novelty uncertainty, predictions or proof obligations, controls, resources, failure modes, and the first decisive milestone. Disclose what was not verified.

**Idea refinement:** Retain the user's core insight and improve its weakest assumptions. Compare the improved proposal against at least one simpler alternative and an explicit null hypothesis.

**Idea portfolio:** Preserve diversity and Pareto tradeoffs rather than returning near-duplicates. On request, output a reproducible JSON candidate ledger compatible with the ranking script and include source links and evaluation notes.

## Optional resources and integrations

- [ideation-protocol.md](references/ideation-protocol.md): cross-domain atom examples, structured divergence, novelty searches, and disciplinary validation.
- [scoring.md](references/scoring.md): score definitions, JSON contract, deterministic ranking, and CLI usage.
- [alien-backend.md](references/alien-backend.md): how to use the original MIT-licensed code and published trained models for ML-literature atom sampling. Do not mistake this pathway for the baseline skill.
- When a GitHub or workspace connector is available, use it to read linked project knowledge and inspect prior art. Use a user-selected project's storage conventions when the user asks you to persist proposals. An idea does not automatically become a new repository.

## Example requests

- "Find five genuinely different FPGA-heavy capstone ideas under a $700 budget and rank them by testability."
- "Extract transferable mechanisms from these three papers and propose new quantum algorithm hypotheses."
- "Critique this computational-physics conjecture. Find its nearest known analogues and design a falsification test."
- "Compare ten product ideas with technical feasibility and nearest existing alternatives."
- "Use the original Alien models for my prepared ML corpus and explain the required resources and permissions."
