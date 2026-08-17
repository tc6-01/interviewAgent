CREATE TABLE interview_directions (
    id TEXT PRIMARY KEY,
    subject_id TEXT NOT NULL REFERENCES subjects(id) ON DELETE CASCADE,
    version INTEGER NOT NULL DEFAULT 1,
    status TEXT NOT NULL CHECK(status IN ('draft', 'confirmed')),
    position TEXT NOT NULL,
    experience_level TEXT NOT NULL,
    focus_areas_json TEXT NOT NULL,
    matched_skills_json TEXT NOT NULL,
    gaps_json TEXT NOT NULL,
    source_sha256 TEXT NOT NULL,
    confirmed_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(id, subject_id)
);

CREATE INDEX idx_interview_directions_subject_updated
    ON interview_directions(subject_id, updated_at DESC);
