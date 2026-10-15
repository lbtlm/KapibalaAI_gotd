ALTER TABLE sequence_steps DROP COLUMN account_id;
ALTER TABLE agent_runs DROP COLUMN admin_account_id;
ALTER TABLE groups DROP COLUMN sequence_config;
ALTER TABLE groups DROP COLUMN admin_account_id;
ALTER TABLE groups DROP COLUMN title;
ALTER TABLE groups DROP COLUMN username;
ALTER TABLE groups DROP COLUMN invite_link;
ALTER TABLE jobs DROP COLUMN logs;
