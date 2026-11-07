PRAGMA foreign_keys=ON;
CREATE TABLE IF NOT EXISTS concepts (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    summary TEXT NOT NULL,
    domain TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS relations (
    source TEXT NOT NULL REFERENCES concepts(id) ON DELETE CASCADE,
    target TEXT NOT NULL REFERENCES concepts(id) ON DELETE CASCADE,
    kind TEXT NOT NULL,
    rationale TEXT NOT NULL,
    PRIMARY KEY (source, target, kind),
    CHECK(source != target)
);
CREATE TABLE IF NOT EXISTS justifications (
    id TEXT PRIMARY KEY,
    concept_id TEXT NOT NULL REFERENCES concepts(id) ON DELETE CASCADE,
    claim TEXT NOT NULL,
    kind TEXT NOT NULL,
    premises_json TEXT NOT NULL,
    explanation TEXT NOT NULL,
    source_reference TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS evidence (
    id TEXT PRIMARY KEY,
    concept_id TEXT NOT NULL REFERENCES concepts(id) ON DELETE CASCADE,
    dimension TEXT NOT NULL,
    question TEXT NOT NULL,
    learner_response TEXT NOT NULL,
    evaluation TEXT NOT NULL,
    passed INTEGER NOT NULL CHECK(passed IN (0,1)),
    confidence REAL NOT NULL CHECK(confidence >= 0 AND confidence <= 1),
    assisted INTEGER NOT NULL DEFAULT 0 CHECK(assisted IN (0,1)),
    recorded_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS evidence_recent ON evidence(concept_id, dimension, recorded_at DESC);
CREATE TABLE IF NOT EXISTS settings (name TEXT PRIMARY KEY, value TEXT NOT NULL);
INSERT OR IGNORE INTO settings(name, value) VALUES ('target_level', '5');
CREATE TABLE IF NOT EXISTS problems (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    statement TEXT NOT NULL,
    discipline TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    mode TEXT NOT NULL,
    difficulty TEXT NOT NULL,
    origin TEXT NOT NULL,
    source_reference TEXT NOT NULL DEFAULT '',
    reference_solution TEXT NOT NULL DEFAULT '',
    grading_rubric TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'open',
    final_evaluation TEXT,
    final_confidence REAL,
    independently_solved INTEGER NOT NULL DEFAULT 0 CHECK(independently_solved IN (0,1)),
    created_at TEXT NOT NULL,
    completed_at TEXT
);
CREATE TABLE IF NOT EXISTS problem_concepts (
    problem_id TEXT NOT NULL REFERENCES problems(id) ON DELETE CASCADE,
    concept_id TEXT NOT NULL REFERENCES concepts(id) ON DELETE RESTRICT,
    credited INTEGER NOT NULL DEFAULT 0 CHECK(credited IN (0,1)),
    PRIMARY KEY (problem_id, concept_id)
);
CREATE INDEX IF NOT EXISTS problem_concepts_by_concept ON problem_concepts(concept_id);
CREATE TABLE IF NOT EXISTS problem_drafts (
    id TEXT PRIMARY KEY,
    problem_id TEXT NOT NULL REFERENCES problems(id) ON DELETE CASCADE,
    body TEXT NOT NULL,
    recorded_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS problem_drafts_recent ON problem_drafts(problem_id, recorded_at);
CREATE TABLE IF NOT EXISTS problem_draft_reflections (
    draft_id TEXT PRIMARY KEY REFERENCES problem_drafts(id) ON DELETE CASCADE,
    learner_reasoning TEXT NOT NULL DEFAULT '',
    self_confidence REAL CHECK(self_confidence BETWEEN 0 AND 1),
    recorded_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS problem_hints (
    id TEXT PRIMARY KEY,
    problem_id TEXT NOT NULL REFERENCES problems(id) ON DELETE CASCADE,
    level INTEGER NOT NULL CHECK(level BETWEEN 0 AND 5),
    guidance TEXT NOT NULL,
    recorded_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS problem_reviews (
    id TEXT PRIMARY KEY,
    problem_id TEXT NOT NULL REFERENCES problems(id) ON DELETE CASCADE,
    draft_id TEXT NOT NULL REFERENCES problem_drafts(id) ON DELETE CASCADE,
    verdict TEXT NOT NULL,
    feedback TEXT NOT NULL,
    recorded_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS learning_sessions (
    id TEXT PRIMARY KEY,
    goal TEXT NOT NULL,
    target_concept_id TEXT NOT NULL REFERENCES concepts(id) ON DELETE RESTRICT,
    mode TEXT NOT NULL CHECK(mode IN ('learn','practise','solve','assess')),
    phase TEXT NOT NULL CHECK(phase IN ('diagnose','plan','teach','practice','reassess','complete')),
    target_level INTEGER NOT NULL CHECK(target_level BETWEEN 1 AND 5),
    plan_version INTEGER NOT NULL DEFAULT 0,
    revision INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS learning_sessions_recent ON learning_sessions(updated_at DESC);
CREATE TABLE IF NOT EXISTS session_probes (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES learning_sessions(id) ON DELETE CASCADE,
    concept_id TEXT NOT NULL REFERENCES concepts(id) ON DELETE RESTRICT,
    question TEXT NOT NULL,
    learner_response TEXT NOT NULL,
    evaluation TEXT NOT NULL,
    outcome TEXT NOT NULL CHECK(outcome IN ('supported','gap','uncertain')),
    confidence REAL NOT NULL CHECK(confidence BETWEEN 0 AND 1),
    assisted INTEGER NOT NULL CHECK(assisted IN (0,1)),
    recorded_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS session_probes_recent ON session_probes(session_id,concept_id,recorded_at DESC);
CREATE TABLE IF NOT EXISTS session_steps (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES learning_sessions(id) ON DELETE CASCADE,
    step_key TEXT NOT NULL,
    kind TEXT NOT NULL CHECK(kind IN ('concept','inference')),
    concept_id TEXT NOT NULL REFERENCES concepts(id) ON DELETE RESTRICT,
    justification_id TEXT REFERENCES justifications(id) ON DELETE RESTRICT,
    title TEXT NOT NULL,
    premises_json TEXT NOT NULL,
    active_version INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE(session_id, step_key)
);
CREATE TABLE IF NOT EXISTS session_plan_versions (
    session_id TEXT NOT NULL REFERENCES learning_sessions(id) ON DELETE CASCADE,
    version INTEGER NOT NULL,
    step_ids_json TEXT NOT NULL,
    recorded_at TEXT NOT NULL,
    PRIMARY KEY(session_id,version)
);
CREATE TABLE IF NOT EXISTS session_step_evidence (
    id TEXT PRIMARY KEY,
    step_id TEXT NOT NULL REFERENCES session_steps(id) ON DELETE CASCADE,
    learner_response TEXT NOT NULL,
    evaluation TEXT NOT NULL,
    verdict TEXT NOT NULL CHECK(verdict IN ('supported','gap','incorrect','uncertain')),
    confidence REAL NOT NULL CHECK(confidence BETWEEN 0 AND 1),
    assisted INTEGER NOT NULL CHECK(assisted IN (0,1)),
    recorded_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS session_step_evidence_recent ON session_step_evidence(step_id,recorded_at DESC);


-- Source material and model-proposed knowledge are not learner mastery evidence.
CREATE TABLE IF NOT EXISTS knowledge_sources (
    id TEXT PRIMARY KEY,
    label TEXT NOT NULL,
    kind TEXT NOT NULL CHECK(kind IN ('document','conversation','notes','web','observation')),
    mime_type TEXT NOT NULL,
    content TEXT NOT NULL,
    sha256 TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE(sha256,kind)
);
CREATE TABLE IF NOT EXISTS source_spans (
    id TEXT PRIMARY KEY,
    source_id TEXT NOT NULL REFERENCES knowledge_sources(id) ON DELETE CASCADE,
    ordinal INTEGER NOT NULL,
    content TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    processed_at TEXT,
    UNIQUE(source_id,ordinal)
);
CREATE INDEX IF NOT EXISTS source_spans_unprocessed ON source_spans(processed_at,source_id,ordinal);
CREATE TABLE IF NOT EXISTS extraction_batches (
    id TEXT PRIMARY KEY,
    span_id TEXT NOT NULL REFERENCES source_spans(id) ON DELETE CASCADE,
    fingerprint TEXT NOT NULL,
    proposals_json TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('pending_audit','accepted','rejected')),
    created_at TEXT NOT NULL,
    resolved_at TEXT,
    UNIQUE(span_id,fingerprint)
);
CREATE TABLE IF NOT EXISTS propositions (
    id TEXT PRIMARY KEY,
    statement TEXT NOT NULL,
    context TEXT NOT NULL,
    domain TEXT NOT NULL,
    identity_hash TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL CHECK(status IN ('provisional','reviewed','disputed')),
    created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS proposition_provenance (
    proposition_id TEXT NOT NULL REFERENCES propositions(id) ON DELETE CASCADE,
    source_span_id TEXT NOT NULL REFERENCES source_spans(id) ON DELETE CASCADE,
    quoted_text TEXT NOT NULL,
    batch_id TEXT NOT NULL REFERENCES extraction_batches(id),
    PRIMARY KEY(proposition_id,source_span_id,quoted_text)
);
CREATE TABLE IF NOT EXISTS proposition_aliases (
    proposition_id TEXT NOT NULL REFERENCES propositions(id) ON DELETE CASCADE,
    statement TEXT NOT NULL,
    source_span_id TEXT NOT NULL REFERENCES source_spans(id) ON DELETE CASCADE,
    PRIMARY KEY(proposition_id,statement,source_span_id)
);
CREATE TABLE IF NOT EXISTS reasoning_chains (
    id TEXT PRIMARY KEY,
    conclusion_id TEXT NOT NULL REFERENCES propositions(id) ON DELETE CASCADE,
    premises_json TEXT NOT NULL,
    relation TEXT NOT NULL CHECK(relation IN ('entails','supports','defines','models')),
    explanation TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('provisional','reviewed','disputed')),
    identity_hash TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS chain_provenance (
    chain_id TEXT NOT NULL REFERENCES reasoning_chains(id) ON DELETE CASCADE,
    source_span_id TEXT NOT NULL REFERENCES source_spans(id) ON DELETE CASCADE,
    batch_id TEXT NOT NULL REFERENCES extraction_batches(id),
    PRIMARY KEY(chain_id,source_span_id)
);
CREATE TABLE IF NOT EXISTS reasoning_chain_review_history (
    id TEXT PRIMARY KEY,
    chain_id TEXT NOT NULL REFERENCES reasoning_chains(id) ON DELETE CASCADE,
    status TEXT NOT NULL CHECK(status IN ('provisional','reviewed','disputed')),
    explanation TEXT NOT NULL,
    recorded_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS reasoning_chain_reviews_recent ON reasoning_chain_review_history(chain_id,recorded_at DESC);

CREATE TABLE IF NOT EXISTS reasoning_formal_checks (
    id TEXT PRIMARY KEY,
    chain_id TEXT NOT NULL REFERENCES reasoning_chains(id) ON DELETE CASCADE,
    submission_hash TEXT NOT NULL,
    claim_snapshot_hash TEXT NOT NULL,
    input_json TEXT NOT NULL,
    result_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE(chain_id,submission_hash)
);
CREATE INDEX IF NOT EXISTS reasoning_formal_checks_recent ON reasoning_formal_checks(chain_id,created_at DESC);

CREATE TABLE IF NOT EXISTS reasoning_translation_reviews (
    id TEXT PRIMARY KEY,
    check_id TEXT NOT NULL REFERENCES reasoning_formal_checks(id) ON DELETE CASCADE,
    chain_id TEXT NOT NULL REFERENCES reasoning_chains(id) ON DELETE CASCADE,
    submission_hash TEXT NOT NULL,
    claim_snapshot_hash TEXT NOT NULL,
    review_json TEXT NOT NULL,
    recorded_at TEXT NOT NULL,
    UNIQUE(check_id,submission_hash)
);
CREATE INDEX IF NOT EXISTS reasoning_translation_reviews_recent ON reasoning_translation_reviews(check_id,recorded_at DESC);


-- A source's original bytes are stored privately; transcription is separate evidence.
CREATE TABLE IF NOT EXISTS attachments (
    id TEXT PRIMARY KEY,
    owner_kind TEXT NOT NULL CHECK(owner_kind IN ('problem','session','source')),
    owner_id TEXT NOT NULL,
    filename TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    sha256 TEXT NOT NULL,
    data BLOB NOT NULL,
    size_bytes INTEGER NOT NULL CHECK(size_bytes>0 AND size_bytes<=10485760),
    note TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    UNIQUE(owner_kind,owner_id,sha256)
);
CREATE INDEX IF NOT EXISTS attachments_owner ON attachments(owner_kind,owner_id,created_at);

-- Practice is a series of distinct retrieval situations, not a count of quiz answers.
CREATE TABLE IF NOT EXISTS practice_sessions (
    id TEXT PRIMARY KEY,
    goal TEXT NOT NULL,
    state TEXT NOT NULL CHECK(state IN ('active','closed')),
    created_at TEXT NOT NULL,
    closed_at TEXT
);
CREATE TABLE IF NOT EXISTS practice_items (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES practice_sessions(id) ON DELETE CASCADE,
    problem_id TEXT NOT NULL UNIQUE REFERENCES problems(id) ON DELETE CASCADE,
    target_concept_id TEXT NOT NULL REFERENCES concepts(id) ON DELETE RESTRICT,
    method TEXT NOT NULL CHECK(method IN ('recall','discrimination','composition','solve')),
    cue_class TEXT NOT NULL,
    difficulty INTEGER NOT NULL CHECK(difficulty BETWEEN 1 AND 5),
    created_at TEXT NOT NULL,
    UNIQUE(session_id,target_concept_id,method,cue_class)
);
CREATE TABLE IF NOT EXISTS practice_outcomes (
    practice_item_id TEXT PRIMARY KEY REFERENCES practice_items(id) ON DELETE CASCADE,
    correct INTEGER NOT NULL CHECK(correct IN (0,1)),
    miss_class TEXT NOT NULL CHECK(miss_class IN ('none','cue','discrimination','execution','careless','structural')),
    highest_hint INTEGER NOT NULL CHECK(highest_hint BETWEEN -1 AND 5),
    remarks TEXT NOT NULL,
    recorded_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS review_schedule (
    concept_id TEXT PRIMARY KEY REFERENCES concepts(id) ON DELETE CASCADE,
    due_at TEXT NOT NULL,
    interval_days INTEGER NOT NULL,
    successes INTEGER NOT NULL,
    last_evaluated_at TEXT NOT NULL
);

-- Durable observation log for incremental extraction across ChatGPT conversations.
CREATE TABLE IF NOT EXISTS learning_events (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES learning_sessions(id) ON DELETE CASCADE,
    origin_id TEXT NOT NULL,
    role TEXT NOT NULL CHECK(role IN ('learner','tutor','tool','source')),
    content TEXT NOT NULL,
    recorded_at TEXT NOT NULL,
    UNIQUE(session_id,origin_id)
);
CREATE TABLE IF NOT EXISTS attachment_annotations (
    id TEXT PRIMARY KEY,
    attachment_id TEXT NOT NULL REFERENCES attachments(id) ON DELETE CASCADE,
    text TEXT NOT NULL,
    method TEXT NOT NULL CHECK(method IN ('model_interpretation','learner_transcription','verified_transcription','observation')),
    uncertainty TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS practice_targets (
    session_id TEXT NOT NULL REFERENCES practice_sessions(id) ON DELETE CASCADE,
    concept_id TEXT NOT NULL REFERENCES concepts(id) ON DELETE RESTRICT,
    PRIMARY KEY(session_id,concept_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS practice_unique_problem_surface ON practice_items(session_id,problem_id);
CREATE TABLE IF NOT EXISTS extraction_review_history (
    id TEXT PRIMARY KEY,
    batch_id TEXT NOT NULL REFERENCES extraction_batches(id) ON DELETE CASCADE,
    action TEXT NOT NULL CHECK(action IN ('reject','accept')),
    reason TEXT NOT NULL,
    recorded_at TEXT NOT NULL
);
-- Indexed source retrieval works without an external embedding service.
CREATE VIRTUAL TABLE IF NOT EXISTS source_search USING fts5(body,span_id UNINDEXED,source_id UNINDEXED,tokenize='unicode61');
CREATE TRIGGER IF NOT EXISTS source_spans_search_insert AFTER INSERT ON source_spans BEGIN
    INSERT INTO source_search(rowid,body,span_id,source_id) VALUES(new.rowid,new.content,new.id,new.source_id);
END;
CREATE TRIGGER IF NOT EXISTS source_spans_search_delete AFTER DELETE ON source_spans BEGIN
    DELETE FROM source_search WHERE rowid=old.rowid;
END;
INSERT INTO source_search(rowid,body,span_id,source_id)
    SELECT sp.rowid,sp.content,sp.id,sp.source_id FROM source_spans sp
    WHERE NOT EXISTS (SELECT 1 FROM source_search si WHERE si.rowid=sp.rowid);
CREATE TABLE IF NOT EXISTS problem_checks (
    id TEXT PRIMARY KEY,
    problem_id TEXT NOT NULL REFERENCES problems(id) ON DELETE CASCADE,
    draft_id TEXT NOT NULL REFERENCES problem_drafts(id) ON DELETE CASCADE,
    method TEXT NOT NULL CHECK(method IN ('model','symbolic','formal','executable','experimental','expert')),
    outcome TEXT NOT NULL CHECK(outcome IN ('pass','fail','inconclusive')),
    evidence_reference TEXT NOT NULL,
    notes TEXT NOT NULL,
    checked_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS problem_checks_by_problem ON problem_checks(problem_id,checked_at DESC);
CREATE TABLE IF NOT EXISTS verified_problem_checks (
    check_id TEXT PRIMARY KEY REFERENCES problem_checks(id) ON DELETE CASCADE,
    verifier_id TEXT NOT NULL,
    receipt_json TEXT NOT NULL,
    signature TEXT NOT NULL,
    recorded_at TEXT NOT NULL,
    UNIQUE(verifier_id,signature)
);
CREATE TABLE IF NOT EXISTS proposition_disputes (
    id TEXT PRIMARY KEY,
    left_id TEXT NOT NULL REFERENCES propositions(id) ON DELETE CASCADE,
    right_id TEXT NOT NULL REFERENCES propositions(id) ON DELETE CASCADE,
    reason TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('unresolved','different_contexts','left_supported','right_supported','both_unsupported')),
    resolution TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    CHECK(left_id!=right_id),
    UNIQUE(left_id,right_id)
);
CREATE INDEX IF NOT EXISTS proposition_disputes_status ON proposition_disputes(status,created_at DESC);
CREATE TABLE IF NOT EXISTS learning_questions (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES learning_sessions(id) ON DELETE CASCADE,
    concept_id TEXT NOT NULL REFERENCES concepts(id) ON DELETE RESTRICT,
    step_id TEXT REFERENCES session_steps(id) ON DELETE SET NULL,
    kind TEXT NOT NULL CHECK(kind IN ('diagnostic','prediction','check')),
    prompt TEXT NOT NULL,
    response_kind TEXT NOT NULL CHECK(response_kind IN ('free_response','multiple_choice')),
    options_json TEXT NOT NULL,
    expected_answer TEXT NOT NULL,
    grading_criteria TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('pending','answered','graded')),
    learner_response TEXT,
    verdict TEXT,
    evaluation TEXT,
    confidence REAL,
    assisted INTEGER NOT NULL CHECK(assisted IN (0,1)) DEFAULT 0,
    created_at TEXT NOT NULL,
    answered_at TEXT,
    graded_at TEXT
);
CREATE INDEX IF NOT EXISTS learning_questions_recent ON learning_questions(session_id,created_at DESC);
CREATE TABLE IF NOT EXISTS question_reflections (
    question_id TEXT PRIMARY KEY REFERENCES learning_questions(id) ON DELETE CASCADE,
    learner_reasoning TEXT NOT NULL DEFAULT '',
    self_confidence REAL CHECK(self_confidence BETWEEN 0 AND 1),
    recorded_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS question_reasoning_reviews (
    question_id TEXT PRIMARY KEY REFERENCES learning_questions(id) ON DELETE CASCADE,
    verdict TEXT NOT NULL CHECK(verdict IN ('supported','gap','uncertain')),
    evaluation TEXT NOT NULL,
    graded_at TEXT NOT NULL
);
