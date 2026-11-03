-- Archiver: terminal jobs finished before their retention.
ALTER TABLE jobs ADD KEY idx_jobs_archive (status, finished_at);
