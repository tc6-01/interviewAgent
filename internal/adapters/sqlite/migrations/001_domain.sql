CREATE TABLE subjects (
    id TEXT PRIMARY KEY,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE profiles (
    subject_id TEXT PRIMARY KEY REFERENCES subjects(id) ON DELETE CASCADE,
    summary_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE question_banks (
    id TEXT PRIMARY KEY,
    subject_id TEXT REFERENCES subjects(id) ON DELETE CASCADE,
    scope TEXT NOT NULL,
    filename TEXT NOT NULL,
    version TEXT NOT NULL DEFAULT '',
    sha256 TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(scope, filename),
    CHECK ((scope = 'builtin' AND subject_id IS NULL) OR (scope = 'user:' || subject_id))
);

CREATE TABLE questions (
    id TEXT PRIMARY KEY,
    bank_id TEXT NOT NULL REFERENCES question_banks(id) ON DELETE CASCADE,
    type TEXT NOT NULL CHECK(type IN ('basic', 'experience', 'design')),
    topic TEXT NOT NULL DEFAULT '',
    question_text TEXT NOT NULL,
    answer_text TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_questions_bank_type ON questions(bank_id, type);
CREATE INDEX idx_question_banks_subject ON question_banks(subject_id);

CREATE TABLE interviews (
    id TEXT PRIMARY KEY,
    subject_id TEXT NOT NULL REFERENCES subjects(id) ON DELETE CASCADE,
    status TEXT NOT NULL,
    summary_json TEXT NOT NULL DEFAULT '{}',
    qa_history_json TEXT NOT NULL DEFAULT '[]',
    ended_reason TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_interviews_subject_created ON interviews(subject_id, created_at DESC);

CREATE TABLE interview_results (
    interview_id TEXT PRIMARY KEY REFERENCES interviews(id) ON DELETE CASCADE,
    report_json TEXT NOT NULL DEFAULT '{}',
    review_plan_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
