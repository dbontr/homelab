BEGIN TRANSACTION;
CREATE TABLE concepts (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    summary TEXT NOT NULL,
    domain TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
INSERT INTO "concepts" VALUES('algebra','Algebra','Relations between quantities','','2026-11-03T04:01:27.862662+00:00');
INSERT INTO "concepts" VALUES('limits','Limits','Behavior near a point','','2026-11-03T04:01:27.865306+00:00');
INSERT INTO "concepts" VALUES('derivatives','Derivatives','Instantaneous rates','','2026-11-03T04:01:27.868029+00:00');
CREATE TABLE evidence (
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
INSERT INTO "evidence" VALUES('eee33c9f-0167-48a0-a6e4-8035a090db47','algebra','recognition','State an equation','x+2=3','Correct',1,0.99,0,'2026-11-03T04:01:27.875748+00:00');
CREATE TABLE justifications (
    id TEXT PRIMARY KEY,
    concept_id TEXT NOT NULL REFERENCES concepts(id) ON DELETE CASCADE,
    claim TEXT NOT NULL,
    kind TEXT NOT NULL,
    premises_json TEXT NOT NULL,
    explanation TEXT NOT NULL,
    source_reference TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
INSERT INTO "justifications" VALUES('93e405ab-765f-41b6-8d94-0e5afa789ce0','derivatives','Derivative from difference quotients','deductive','["limits"]','Take the limit of the average rate','','2026-11-03T04:01:27.873998+00:00');
CREATE TABLE learning_sessions (
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
INSERT INTO "learning_sessions" VALUES('05759898-a43d-4f7e-bd13-c603b4c1881b','Reconstruct a derivative','derivatives','learn','plan',5,1,3,'2026-11-03T04:01:27.878544+00:00','2026-11-03T04:01:27.886886+00:00');
CREATE TABLE problem_concepts (
    problem_id TEXT NOT NULL REFERENCES problems(id) ON DELETE CASCADE,
    concept_id TEXT NOT NULL REFERENCES concepts(id) ON DELETE RESTRICT,
    credited INTEGER NOT NULL DEFAULT 0 CHECK(credited IN (0,1)),
    PRIMARY KEY (problem_id, concept_id)
);
INSERT INTO "problem_concepts" VALUES('3132f3f8-59ed-4e06-8792-713fa5ea310a','limits',0);
CREATE TABLE problem_drafts (
    id TEXT PRIMARY KEY,
    problem_id TEXT NOT NULL REFERENCES problems(id) ON DELETE CASCADE,
    body TEXT NOT NULL,
    recorded_at TEXT NOT NULL
);
INSERT INTO "problem_drafts" VALUES('00155d54-b55a-438d-bdc4-4df07ffef5d3','3132f3f8-59ed-4e06-8792-713fa5ea310a','Attempt a compactness argument','2026-11-03T04:01:27.892475+00:00');
CREATE TABLE problem_hints (
    id TEXT PRIMARY KEY,
    problem_id TEXT NOT NULL REFERENCES problems(id) ON DELETE CASCADE,
    level INTEGER NOT NULL CHECK(level BETWEEN 0 AND 5),
    guidance TEXT NOT NULL,
    recorded_at TEXT NOT NULL
);
CREATE TABLE problem_reviews (
    id TEXT PRIMARY KEY,
    problem_id TEXT NOT NULL REFERENCES problems(id) ON DELETE CASCADE,
    draft_id TEXT NOT NULL REFERENCES problem_drafts(id) ON DELETE CASCADE,
    verdict TEXT NOT NULL,
    feedback TEXT NOT NULL,
    recorded_at TEXT NOT NULL
);
CREATE TABLE problems (
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
INSERT INTO "problems" VALUES('3132f3f8-59ed-4e06-8792-713fa5ea310a','Real analysis proof','Show uniform convergence','','proof','independent','challenging','generated','','Use nested compact sets','Control quantifiers','open',NULL,NULL,0,'2026-11-03T04:01:27.890210+00:00',NULL);
CREATE TABLE relations (
    source TEXT NOT NULL REFERENCES concepts(id) ON DELETE CASCADE,
    target TEXT NOT NULL REFERENCES concepts(id) ON DELETE CASCADE,
    kind TEXT NOT NULL,
    rationale TEXT NOT NULL,
    PRIMARY KEY (source, target, kind),
    CHECK(source != target)
);
INSERT INTO "relations" VALUES('limits','algebra','requires','Manipulate difference quotients');
INSERT INTO "relations" VALUES('derivatives','limits','requires','Instantaneous rate uses a limit');
CREATE TABLE session_plan_versions (
    session_id TEXT NOT NULL REFERENCES learning_sessions(id) ON DELETE CASCADE,
    version INTEGER NOT NULL,
    step_ids_json TEXT NOT NULL,
    recorded_at TEXT NOT NULL,
    PRIMARY KEY(session_id,version)
);
INSERT INTO "session_plan_versions" VALUES('05759898-a43d-4f7e-bd13-c603b4c1881b',1,'["40a1c871-1f8b-4d71-82fd-006587580e09", "c2d760f5-5146-41a9-b559-7d2e79ce0229", "2b84df56-bdd0-4fb9-bc5f-c367f7762dc8", "4d371ad5-0c3a-48bb-a688-502bd384c050"]','2026-11-03T04:01:27.886854+00:00');
CREATE TABLE session_probes (
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
INSERT INTO "session_probes" VALUES('94411ae2-e7bc-45d1-a5eb-aba4199c3233','05759898-a43d-4f7e-bd13-c603b4c1881b','derivatives','How is a derivative defined?','By a limit','Needs reconstruction','gap',0.9,0,'2026-11-03T04:01:27.881547+00:00');
CREATE TABLE session_step_evidence (
    id TEXT PRIMARY KEY,
    step_id TEXT NOT NULL REFERENCES session_steps(id) ON DELETE CASCADE,
    learner_response TEXT NOT NULL,
    evaluation TEXT NOT NULL,
    verdict TEXT NOT NULL CHECK(verdict IN ('supported','gap','incorrect','uncertain')),
    confidence REAL NOT NULL CHECK(confidence BETWEEN 0 AND 1),
    assisted INTEGER NOT NULL CHECK(assisted IN (0,1)),
    recorded_at TEXT NOT NULL
);
CREATE TABLE session_steps (
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
INSERT INTO "session_steps" VALUES('40a1c871-1f8b-4d71-82fd-006587580e09','05759898-a43d-4f7e-bd13-c603b4c1881b','concept:algebra','concept','algebra',NULL,'Algebra','[]',1,'2026-11-03T04:01:27.886763+00:00');
INSERT INTO "session_steps" VALUES('c2d760f5-5146-41a9-b559-7d2e79ce0229','05759898-a43d-4f7e-bd13-c603b4c1881b','concept:limits','concept','limits',NULL,'Limits','["algebra"]',1,'2026-11-03T04:01:27.886812+00:00');
INSERT INTO "session_steps" VALUES('2b84df56-bdd0-4fb9-bc5f-c367f7762dc8','05759898-a43d-4f7e-bd13-c603b4c1881b','inference:93e405ab-765f-41b6-8d94-0e5afa789ce0','inference','derivatives','93e405ab-765f-41b6-8d94-0e5afa789ce0','Derivative from difference quotients','["limits"]',1,'2026-11-03T04:01:27.886829+00:00');
INSERT INTO "session_steps" VALUES('4d371ad5-0c3a-48bb-a688-502bd384c050','05759898-a43d-4f7e-bd13-c603b4c1881b','concept:derivatives','concept','derivatives',NULL,'Derivatives','["limits"]',1,'2026-11-03T04:01:27.886845+00:00');
CREATE TABLE settings (name TEXT PRIMARY KEY, value TEXT NOT NULL);
INSERT INTO "settings" VALUES('target_level','5');
CREATE INDEX evidence_recent ON evidence(concept_id, dimension, recorded_at DESC);
CREATE INDEX problem_concepts_by_concept ON problem_concepts(concept_id);
CREATE INDEX problem_drafts_recent ON problem_drafts(problem_id, recorded_at);
CREATE INDEX learning_sessions_recent ON learning_sessions(updated_at DESC);
CREATE INDEX session_probes_recent ON session_probes(session_id,concept_id,recorded_at DESC);
CREATE INDEX session_step_evidence_recent ON session_step_evidence(step_id,recorded_at DESC);
COMMIT;
