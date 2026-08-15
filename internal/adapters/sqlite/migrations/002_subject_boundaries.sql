CREATE TABLE question_banks_v2 (
    id TEXT PRIMARY KEY,
    subject_id TEXT REFERENCES subjects(id) ON DELETE CASCADE,
    scope TEXT NOT NULL,
    filename TEXT NOT NULL,
    version TEXT NOT NULL DEFAULT '',
    sha256 TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(scope, filename),
    CHECK (
        (scope = 'builtin' AND subject_id IS NULL)
        OR
        (subject_id IS NOT NULL AND length(trim(subject_id)) > 0 AND scope = ('user:' || subject_id))
    )
);

INSERT INTO question_banks_v2(id, subject_id, scope, filename, version, sha256, created_at, updated_at)
SELECT id, subject_id, scope, filename, version, sha256, created_at, updated_at
FROM question_banks;

CREATE TABLE questions_v2 (
    id TEXT PRIMARY KEY,
    bank_id TEXT NOT NULL REFERENCES question_banks_v2(id) ON DELETE CASCADE,
    type TEXT NOT NULL CHECK(type IN ('basic', 'experience', 'design')),
    topic TEXT NOT NULL DEFAULT '',
    question_text TEXT NOT NULL,
    answer_text TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO questions_v2(id, bank_id, type, topic, question_text, answer_text, source, created_at, updated_at)
SELECT id, bank_id, type, topic, question_text, answer_text, source, created_at, updated_at
FROM questions;

DROP TABLE questions;
DROP TABLE question_banks;
ALTER TABLE question_banks_v2 RENAME TO question_banks;
ALTER TABLE questions_v2 RENAME TO questions;
CREATE INDEX idx_questions_bank_type ON questions(bank_id, type);
CREATE INDEX idx_question_banks_subject ON question_banks(subject_id);

CREATE TABLE interviews_v2 (
    id TEXT PRIMARY KEY,
    subject_id TEXT NOT NULL REFERENCES subjects(id) ON DELETE CASCADE,
    status TEXT NOT NULL,
    summary_json TEXT NOT NULL DEFAULT '{}',
    qa_history_json TEXT NOT NULL DEFAULT '[]',
    ended_reason TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(id, subject_id)
);

INSERT INTO interviews_v2(id, subject_id, status, summary_json, qa_history_json, ended_reason, created_at, updated_at)
SELECT id, subject_id, status, summary_json, qa_history_json, ended_reason, created_at, updated_at
FROM interviews;

CREATE TABLE interview_results_v2 (
    interview_id TEXT PRIMARY KEY,
    subject_id TEXT NOT NULL,
    report_json TEXT NOT NULL DEFAULT '{}',
    review_plan_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(interview_id, subject_id) REFERENCES interviews_v2(id, subject_id) ON DELETE CASCADE
);

INSERT INTO interview_results_v2(interview_id, subject_id, report_json, review_plan_json, created_at, updated_at)
SELECT r.interview_id, i.subject_id, r.report_json, r.review_plan_json, r.created_at, r.updated_at
FROM interview_results r
JOIN interviews i ON i.id = r.interview_id;

DROP TABLE interview_results;
DROP TABLE interviews;
ALTER TABLE interviews_v2 RENAME TO interviews;
ALTER TABLE interview_results_v2 RENAME TO interview_results;
CREATE INDEX idx_interviews_subject_created ON interviews(subject_id, created_at DESC);
