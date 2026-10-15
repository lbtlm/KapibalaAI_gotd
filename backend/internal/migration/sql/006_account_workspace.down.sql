DROP TABLE account_logs,account_group_snapshots,account_imports;
ALTER TABLE accounts DROP COLUMN connection_id,DROP COLUMN created_at,DROP COLUMN last_check_error,DROP COLUMN last_check_result,DROP COLUMN last_checked_at,DROP COLUMN premium,DROP COLUMN last_name,DROP COLUMN first_name,DROP COLUMN username,DROP COLUMN verification_source,DROP COLUMN phone_verified,DROP COLUMN phone,DROP COLUMN group_id;
DROP TABLE account_groups;
