# Educational evaluation

Noesis has two distinct evaluation questions. Software contract tests ask whether the learning system **records and interprets evidence correctly**. Learner-outcome studies ask whether the tutoring experience **actually improves independent understanding**. A successful contract test does not answer the second question.

## Reproducible contract cases

The cases in `internal/store/testdata/pedagogy_scenarios.json` contain subject-specific problems, exact answer keys, synthetic learner replies, explanatory arguments, preassigned reasoning judgments, confidence reports and expected state transitions. These examples are curated fixtures, not real learner observations or evidence that a language model generated a sound assessment.

Run `go test -race -count=1 ./internal/store -run 'Test(Pedagogy|Delayed)' -v` to exercise the curriculum cases and assessment safeguards. The suite covers mathematics, physics, electrical engineering, biology, historical reasoning, practical crafts, computer science, linguistics, probability and logic. It verifies:

- Pending questions remain sealed and resumable until the learner answers and a grading decision is recorded.
- A correct choice with a deficient, missing or unexamined explanation does not establish a secure diagnostic prerequisite.
- Supported explanations can guide the next learning step, but diagnostic answers never become global mastery evidence.
- An assisted exercise or a repeated problem statement cannot manufacture independent transfer credit.
- Learner confidence remains separate from objective correctness and provisional evaluator confidence.
- A delayed-retention pass must be unassisted and occur at least 24 hours after the latest qualifying independent reconstruction. A later reconstruction requires a fresh delayed check. The due queue does not silently retain stale successes.
- Contradictory authenticated verifier outcomes block the highest mastery threshold.

Add scenarios with a genuine new reasoning failure, not copies that change only nouns. Use a fixed answer key and a reviewable rationale before writing expected outcomes. Keep fixtures synthetic: never commit private learner messages, photographs, solutions or actual human assessment records into the source repository.

This suite tests the deterministic store, orchestration and assessment boundary. It does **not** test whether the language model recognizes an unsupported explanation; its reasoning verdict is supplied by the fixture. A well-performing model may still give incorrect instruction or misleading explanations.

## Model-behavior assessment

The [blinded tutoring-model evaluation](model-evaluation.md) runs a controlled, two-condition policy comparison with real model completions, a separate reference rubric and two independent human reviewers per case. It reports misconception recognition, factual accuracy, explanatory repair, transfer prompts and evidentiary restraint. These outcomes describe the model's *behavior* in scripted exchanges, not a human learner's achievement. Rubrics and reviewer judgments are never supplied to the tested model.

## End-to-end tutoring effectiveness protocol

For each domain, use a common study design rather than assuming every discipline requires formal proof:

1. **Baseline.** Give unfamiliar, independently answerable problems before instruction. Assess final answers, reasoning steps, stated assumptions, and self-reported confidence separately. Record whether the learner received hints or external help.
2. **Instruction.** Start from demonstrated prerequisites, select an explanatory route, require the learner to justify each important inference, and revisit the earliest unresolved gap. Log the teaching interactions and all assistance, including model suggestions.
3. **Immediate transfer.** Give new problems that differ in causal structure, surface presentation or practical context from the teaching examples. Do not reuse the exact numerical values or a transparently copied template.
4. **Delayed transfer and retention.** Reassess after at least 24 hours without a provided answer key. Prefer an additional later measurement at roughly one week. New reconstructions reset the relevant retention checkpoint.
5. **Independent evaluation.** Before showing tutoring transcripts, have qualified graders define a rubric, establish objective answers when possible, and rate reasoning validity, transfer and missing premises. Use two independent reviewers for disputed interpretations and retain unresolved disagreement.
6. **Comparative evaluation.** Compare performance against a defensible baseline or an alternative instruction condition, with comparable prior experience and problem difficulty. Do not infer learning improvement from post-test accuracy alone.

Domain-specific rubrics matter. Mathematical proof requires valid inferences; experimental science requires evidence and uncertainty; history requires source criticism; electrical engineering requires correct physical assumptions and units; practical crafts require safe observable performance. A model's confidence in grading is not a substitute for an independent standard.

## Metrics and decision rules

Report per-domain and aggregated results, including:

- Pretest, immediate transfer and delayed transfer accuracy; an explicit denominator for each.
- Independently justified inference steps, missing prerequisite repairs, and number of false-positive mastery declarations.
- Problems solved without hints, novelty of holdout problems and success after later retrieval.
- Confidence calibration and confident errors, distinguishing objectively keyed answers from authenticated verifier checks.
- Human-assessor agreement, unscorable responses, incomplete sessions and missing delayed assessments.

Treat missing data as missing, not as success or failure. Report uncertainty and sample size. A single simulated learner, one example problem or a small uncontrolled before-and-after comparison cannot establish general effectiveness. Record the actual model version, teaching condition, rubric version, holdout provenance and run date for reproducibility, without exposing learner identity.

## Release criteria

Production readiness requires a verified authentication boundary, a complete ChatGPT-host interaction test, private-data backup/restore, stable simulated contract results, and evidence that real learners can improve on genuinely unfamiliar and delayed problems without fabricated credit. A reproducible synthetic pass is necessary for software assurance but insufficient for pedagogical validation.

Do not deploy or enroll trusted production verifiers simply because the synthetic acceptance suite passes.
