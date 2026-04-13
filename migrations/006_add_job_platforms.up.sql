ALTER TABLE jobs
ADD COLUMN platforms JSON NULL AFTER execution_plan;
