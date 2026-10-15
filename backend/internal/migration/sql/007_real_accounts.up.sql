ALTER TABLE account_group_snapshots ALTER COLUMN last_synced_at DROP NOT NULL;
WITH affected AS MATERIALIZED (
    SELECT id,status FROM accounts WHERE verification_source='simulated' FOR UPDATE
), changed AS (
    UPDATE accounts a SET
        status=CASE WHEN old.status IN ('online','rate_limited') THEN 'disconnected' ELSE old.status END,
        connection_id=NULL,rate_limited_until=NULL,
        phone_verified=false,verification_source=NULL,
        last_checked_at=CASE WHEN a.last_check_result='success' THEN NULL ELSE a.last_checked_at END,
        last_check_error=CASE WHEN a.last_check_result='success' THEN NULL ELSE a.last_check_error END,
        last_check_result=CASE WHEN a.last_check_result='success' THEN NULL ELSE a.last_check_result END
    FROM affected old WHERE a.id=old.id RETURNING a.id,old.status AS previous_status
), clock AS (
    UPDATE ws_clock SET seq=seq+(SELECT count(*) FROM changed WHERE previous_status IN ('online','rate_limited'))
    WHERE id=1 RETURNING seq
), events AS (
    INSERT INTO ws_events(seq,type,payload)
    SELECT clock.seq-(SELECT count(*) FROM changed WHERE previous_status IN ('online','rate_limited'))+row_number() OVER (ORDER BY changed.id),
        'account_status_changed',jsonb_build_object('accountId',changed.id,'from',changed.previous_status,'to','disconnected')
    FROM changed CROSS JOIN clock WHERE previous_status IN ('online','rate_limited') RETURNING seq
), cache_history AS (
    INSERT INTO account_logs(account_id,actor,action,result,created_at)
    SELECT s.account_id,'system','legacy_cached_groups_unverified','success',s.last_synced_at
    FROM account_group_snapshots s JOIN changed a ON a.id=s.account_id WHERE s.last_synced_at IS NOT NULL RETURNING id
), cache AS (
    UPDATE account_group_snapshots s SET last_synced_at=NULL FROM changed a WHERE s.account_id=a.id RETURNING s.account_id
), logs AS (
    INSERT INTO account_logs(account_id,actor,action,result)
    SELECT id,'system','legacy_verification_reset','success' FROM changed RETURNING id
)
INSERT INTO gateway_actions(id,account_id,action)
SELECT 'real-only-upgrade:'||id,id,'disconnect' FROM changed WHERE previous_status IN ('online','rate_limited');
ALTER TABLE accounts DROP CONSTRAINT accounts_verification_source_check;
ALTER TABLE accounts ADD CONSTRAINT accounts_verification_source_check CHECK(verification_source='telegram');
