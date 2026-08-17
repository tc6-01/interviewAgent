ALTER TABLE interview_directions ADD COLUMN jd_analysis_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE interview_directions ADD COLUMN resume_match_json TEXT NOT NULL DEFAULT '{}';
