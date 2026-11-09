# Adaptive Teaching MCP tool reference

All tools operate on the authenticated server's private SQLite store. ChatGPT provides pedagogy and provisional evaluation; tools do not invoke a model API.

## Knowledge graph

| Tool | Purpose |
| --- | --- |
| `get_learning_summary` | Read current mastery target, concept coverage, and problem counts |
| `add_concept` | Add a curriculum proposition or concept without granting credit |
| `connect_concepts` | Relate concepts with a reasoned dependency or analogy |
| `add_justification` | Attach a typed deduction, empirical ground, axiom, definition, model, or heuristic |
| `get_concept` | Read a concept, foundations, justifications and two-part mastery evidence |
| `get_graph` | Retrieve a bounded neighborhood of graph nodes and links |
| `record_assessment` | Preserve a genuine learner answer and a provisional conceptual evaluation |
| `set_learning_target` | Change target 1–5 without modifying older evidence |
| `recommend_learning` | Suggest prerequisites and identify unsupported conceptual targets |
| `show_knowledge_graph` | Open the interactive graph in an MCP Apps host |
| `show_practice_question` | Open a short conceptual question and return its answer to the host |

## Adaptive teaching sessions

| Tool | Purpose |
| --- | --- |
| `start_learning_session` | Bind a goal to an existing concept; choose Learn, Practise, Solve, or Assess and a session-specific mastery target |
| `list_learning_sessions` | Find interrupted or completed sessions without replacing learner work |
| `get_learning_session` | Read session phase, revision, diagnostic answers, active plan, and individual inference evidence |
| `advance_learning_session` | Change phase through validated transitions; optionally require an exact revision |
| `next_learning_action` | Return the session's next learning action, pending-question status, reasoning obstruction, or reassessment need, with a revision for conflict detection; no mastery is awarded |
| `recommend_session_probe` | Return the nearest unresolved concept or a recommendation to teach rather than re-probe |
| `record_session_probe` | Persist the genuine learner response and provisional diagnostic assessment; does not award mastery |
| `build_learning_plan` | Plan prerequisites and selected reasoning route, preserving previously recorded evidence across revisions |
| `record_learning_step` | Record learner-authored reasoning on an explicit concept or inference step, separately from concept mastery |
| `show_learning_session` | Render the adaptive learning route and its evidence in compatible MCP Apps clients |
| `open_learning_question` | Seal a diagnostic, prediction or check question, expected answer and grading rubric before display; one unresolved question per learning session |
| `get_learning_question`, `get_pending_learning_question` | Resume an unanswered or ungraded question without disclosing its answer key |
| `answer_learning_question` | Save the learner's original answer, optional explanatory reasoning and optional learner-reported confidence (0–1), exactly once, before grading |
| `grade_learning_question` | Grade answer correctness and optional explanation independently; a correct diagnostic choice without an assessed supporting reason is inconclusive as prerequisite evidence |
| `show_learning_question` | Display a committed, resumable learning question in the embedded MCP App |
| `get_confidence_calibration` | Return separate Brier scores and reliability bands for unassisted precommitted choices and authenticated checker outcomes with learner-reported certainty; read-only and never awards mastery |

The default Learn path is `diagnose → plan → teach → practice → reassess → complete`. The system allows specified backward transitions to repair a gap. The mode is fixed at session creation. Practise and Solve sessions can begin in practice; Assess begins in reassessment. A session may finish without any mastery credit. A session cannot change phase while a question remains unanswered or ungraded. The question key and rubric are disclosed only after the question is graded. Prediction and check outcomes are separate from concept proficiency; diagnostic question grades also update the session probe history. A model cannot alter the committed multiple-choice key after seeing the response.

`build_learning_plan` considers both graph prerequisites and multi-premise justifications. Its optional `preferred_justifications` maps a concept ID to a justification ID; otherwise it chooses a route with fewer unresolved premises. Correct diagnostic answers are useful for deciding what to teach, but are not by themselves durable verification of prerequisites. Reasoning steps preserve independent and assisted assessments when the route is rebuilt. A supported step is still a provisional model judgment, not a proof certificate or a concept mastery award.

## Extended problem solving

| Tool | Purpose |
| --- | --- |
| `create_problem` | Create a durable proof/calculation/programming problem; generated problems require a reference solution and rubric |
| `get_problem` | Read problem statement, all saved revisions, hint/review history, and outcome, without reference solution |
| `list_problems` | Find saved open or closed exercises |
| `save_problem_draft` | Append a learner-authored answer, optional reasoning and learner-reported confidence (0–1) as one immutable revision; exact consecutive repeats are deduplicated |
| `record_problem_hint` | Persist the exact R0–R5 assistance; not allowed for exam problems |
| `review_problem_draft` | Persist feedback linked to the latest saved revision; not allowed for exam problems |
| `finish_problem` | Close a problem with solved/incomplete/abandoned outcome, evaluation, confidence, and justified concept credits |
| `get_problem_solution` | Retrieve the sealed reference solution and rubric only after closure |
| `show_problem_workbench` | Open the persistent problem workspace inside compatible MCP Apps clients |

### Problem creation

`create_problem` accepts `title`, `statement`, `kind`, `mode`, `difficulty`, `concept_ids`, `discipline`, `origin`, `reference_solution`, `grading_rubric`, and `source_reference`. Kinds are proof, calculation, derivation, programming, scientific, or other. Modes are independent, guided, or exam. Difficulty is foundational, standard, challenging, or research. `origin=generated` requires both a nonempty reference solution and a rubric. `origin=user` permits empty reference material, which must not be fabricated.

### Independent credit

`finish_problem` requires `problem_id`, `outcome`, `evaluation`, and `confidence` between 0 and 1. `credited_concept_ids` is optional, but the credits must refer to concepts linked at problem creation. Credit is permitted only after an independently solved, confident attempt: outcome solved, confidence at least 0.75, mode independent/exam, and no hints or interim reviews. At target 4, at least one independently solved substantial problem is required. At target 5, at least three distinct challenging/research problems, varied contexts, delayed retention and an explicitly attributed corroborating check are required in addition to conceptual evidence. Legacy counters remain in the read API but are not sufficient to certify full mastery. A problem can be solved with assistance without earning independent credit.

`get_problem` and `list_problems` never disclose `reference_solution` or `grading_rubric`. `get_problem_solution` refuses access while the problem is open; it is available after closure, including when an attempt was abandoned. All grades remain provisional model assessments, not machine-checked proofs.

The graph records *what is justified*. Conceptual assessment and independent problem-solving credits are separate and must not be conflated.

## Source-grounded knowledge and retrieval

| Tool | Purpose |
| --- | --- |
| `ingest_source_text` | Save a source text as immutable UTF-8 spans; no mastery is conferred |
| `list_learning_sources`, `show_learning_sources` | List imported sources, covered and pending spans, and open the source library |
| `next_extraction` | Return the next unprocessed source span, pending source audit, or completion |
| `propose_extraction` | Save an extractor's zero-indexed claim candidates, verbatim quotations and joint-premise chains |
| `audit_extraction`, `reject_extraction` | Independently review source faithfulness and proposition identity, then merge or reject |
| `get_reasoning_graph`, `show_reasoning_graph` | Explore propositions and joint-premise chains with current review eligibility, blockers, and standing |
| `get_reasoning_chain` | Inspect every required premise, original chain provenance, formal checks and append-only reasoning-chain review history |
| `check_reasoning_entailment` | Check a caller-supplied propositional formalization against all truth assignments, recording countermodels, inconsistent premises, and redundant premises |
| `get_reasoning_formal_checks` | Read immutable checks, exact formula/atom meanings, attached translation reviews, and stale-claim indicators |
| `record_translation_review` | Record an explicit, source-anchored judgment for all premise and conclusion mappings of one formal check |
| `get_translation_reviews` | Inspect append-only mapping judgments, original citation anchors, assumptions, limitations, and current integrity |
| `get_proposition`, `review_proposition`, `review_reasoning_chain` | Inspect attributed evidence; review a chain only after all its claims are reviewed and its current evidence has no blocking disputes |
| `record_proposition_dispute`, `resolve_proposition_dispute` | Preserve competing explanations and record contextual reconciliation without granting mastery |
| `search_learning_sources`, `search_knowledge` | Literal substring or local FTS5-ranked source retrieval with provenance |
| `append_learning_event`, `get_learning_events` | Persist genuine learning-session messages for incremental auditable extraction |

`ingest_source_text` takes `label`, `kind` (`document`, `conversation`, `notes`, `web`, `observation`), `mime_type` (`text/plain`/`text/markdown`), and `text` (maximum 2 MiB). A `next_extraction` span has a stable `span_id`, content and fingerprint. In `propose_extraction`, `claims` are `{statement,context,domain,quote}`; quotations must be verbatim in the specified span. Each `chains` element contains `{conclusion, premises,relation,explanation}` referencing indexes in the submitted claims. Relations are `entails`, `supports`, `defines` and `models`.

`audit_extraction` requires indexed decisions for every proposed claim (`accept`, `reuse` with `reuse_id` and a rationale, or `reject`) and every proposed chain (`keep=true/false`). The audit rechecks exact source-text integrity, span identity, and quotation presence before merging. Claims remain provisional; an audit does not establish truth in every context, and no claim changes learner mastery. Rejected batches can be corrected and resubmitted. Extraction is host-operated; there is no inference daemon secretly processing documents.

`review_reasoning_chain(chain_id,status,explanation)` records an append-only rationale. A `reviewed` transition requires separate review of the conclusion and **every** premise, intact attributed source spans, an accepted chain extraction, and no disqualifying disputes. `get_reasoning_chain(chain_id)` returns the joint premise set, chain provenance, review history and current blockers. `get_reasoning_graph` distinguishes stored `status` from `review_active`: later contradictions or corrupted citations can block an old review without rewriting history. The eligibility check is procedural, not a proof of mathematical entailment, source reliability, or learner mastery.

Translation reviews are created with `record_translation_review(check_id,review)`. The review contains `decision` (`aligned`, `misaligned` or `uncertain`), a user-supplied `reviewer_ref`, `reason`, `domain` (`propositional`, `quantified`, `mathematical`, `empirical`, `model_dependent` or `historical`), optional `assumptions` and `limitations` arrays, and `anchors`. There must be exactly one anchor for each stored premise and conclusion. Each anchor contains `proposition_id`, `source_span_id`, `quoted_text`, `alignment`, and `explanation`. Obtain valid `source_span_id` and `quoted_text` pairs from `get_proposition`'s `provenance` array. The server verifies these exact tuples against accepted extraction batches, unchanged source spans, and their original source content; a review may dissent without erasing earlier reviews. Claims and source bytes are rechecked on retrieval and an obsolete review is marked stale. Review submissions are idempotent.

A non-propositional `domain` requires explicit validation limitations. Empirical and model-dependent judgments additionally require explicit assumptions. These are **completeness checks on the review record**, not scientific or historical verification. `reviewer_ref` is user-supplied and does not authenticate a human or an independent model. Review results always return `reviewer_authenticated=false`, `translation_verified=false`, and `semantic_validity_certified=false`; no result awards mastery or changes the chain's review standing. Automated quantified logic, quantitative scientific validation and independent reviewer authentication remain outside this protocol.

## Original material and interpretation

| Tool | Purpose |
| --- | --- |
| `upload_attachment` | Store original base64 file bytes with MIME/signature validation for a problem, session, or source |
| `list_attachments`, `get_attachment` | Retrieve metadata or, on explicit request, the original base64 |
| `inspect_attachment` | Present an original image as MCP image content to a compatible host model |
| `annotate_attachment` | Distinguish model observation, learner transcription, verified transcription and uncertain interpretations |

`upload_attachment` accepts `owner_kind`, `owner_id`, `filename`, `mime_type`, `base64`, and an optional note. Per-file storage limit: 10 MiB; the embedded UI currently limits selections to 5 MiB per file to reduce client-side transport risk. The source file is not automatically understood merely because it is stored. The tool accepts PNG, JPEG, WebP, HEIC, PDF, text/plain and text/markdown files; PDF interpretation needs an approved reader.

## Interleaved practice and mastery evidence

| Tool | Purpose |
| --- | --- |
| `start_practice_session`, `get_practice_session`, `list_practice_sessions`, `show_practice_session` | Store and reopen a practice trajectory with exact targets and outcomes |
| `recommend_practice` | Recommend a target, recall/discrimination/composition/solve mode, difficulty and cue classes to avoid |
| `create_practice_problem` | Register a single fresh problem with its precommitted solution and rubric; never allows a second active problem |
| `record_practice_outcome` | Classify an already concluded problem as correct or missed, record its hint level, and update the delayed retrieval schedule |
| `get_review_queue`, `get_reassessment_queue` | Retrieve due and upcoming review obligations, without creating a background notification task |
| `record_problem_check` | Save a provisional model or attributed checker claim about the latest learner draft; the claim does not count as trusted corroboration |
| `record_verified_problem_check` | Import an Ed25519-signed checker receipt, bound to the exact problem, latest draft, method, outcome and evidence digest |
| `get_mastery_portfolio` | Inspect independent-work evidence and distinguish attributed claims from authenticated checker receipts |
| `close_practice_session` | Finish a practice session without deleting unsuccessful attempts |

Practice methods are `recall`, `discrimination`, `composition` and `solve`. Miss classifications are `cue`, `discrimination`, `execution`, `careless` and `structural`; a correct outcome uses `none`. Hints use the R0-R5 scale in the main problem store. No practice outcome automatically overrides source review or conceptual reconstruction evidence. At the highest target, at least three distinct independently completed challenging/research problems, varied contexts, delayed retention and authenticated checker corroboration are required in addition to conceptual dimensions. This is an auditable evidence threshold, not an automatic proof certificate.
