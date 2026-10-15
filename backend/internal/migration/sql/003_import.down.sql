DELETE FROM groups WHERE creator_account_id IS NULL;
ALTER TABLE groups ALTER COLUMN creator_account_id SET NOT NULL;
