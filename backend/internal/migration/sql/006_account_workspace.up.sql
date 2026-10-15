CREATE TABLE account_groups(id text PRIMARY KEY,name text NOT NULL UNIQUE CHECK(length(name) BETWEEN 1 AND 100),created_at timestamptz NOT NULL DEFAULT now());
ALTER TABLE accounts ADD COLUMN group_id text REFERENCES account_groups ON DELETE SET NULL;
ALTER TABLE accounts ADD COLUMN phone text;
ALTER TABLE accounts ADD COLUMN phone_verified boolean NOT NULL DEFAULT false;
ALTER TABLE accounts ADD COLUMN verification_source text CHECK(verification_source IN ('telegram','simulated'));
ALTER TABLE accounts ADD COLUMN username text;
ALTER TABLE accounts ADD COLUMN first_name text;
ALTER TABLE accounts ADD COLUMN last_name text;
ALTER TABLE accounts ADD COLUMN premium boolean;
ALTER TABLE accounts ADD COLUMN last_checked_at timestamptz;
ALTER TABLE accounts ADD COLUMN last_check_result text;
ALTER TABLE accounts ADD COLUMN last_check_error text;
ALTER TABLE accounts ADD COLUMN created_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE accounts ADD COLUMN connection_id text;
CREATE TABLE account_imports(session_fingerprint text PRIMARY KEY,config_fingerprint text NOT NULL,account_id text NOT NULL UNIQUE,completed boolean NOT NULL DEFAULT false,created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE account_group_snapshots(account_id text PRIMARY KEY REFERENCES accounts,items jsonb NOT NULL,last_synced_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE account_logs(id bigserial PRIMARY KEY,account_id text NOT NULL REFERENCES accounts,actor text NOT NULL,action text NOT NULL,result text NOT NULL CHECK(result IN ('success','failed')),error_code text,created_at timestamptz NOT NULL DEFAULT now());
CREATE INDEX account_logs_page ON account_logs(account_id,id DESC);

-- Existing live clients have no fenced incarnation. Converge to explicit
-- reconnection while retaining queued sends, memberships and effect identities.
WITH legacy AS MATERIALIZED (
    SELECT id,status FROM accounts WHERE status IN ('online','rate_limited') FOR UPDATE
), changed AS (
    UPDATE accounts a SET status='disconnected',rate_limited_until=NULL
    FROM legacy l WHERE a.id=l.id RETURNING a.id,l.status AS previous_status
), clock AS (
    UPDATE ws_clock SET seq=seq+(SELECT count(*) FROM changed) WHERE id=1 RETURNING seq
), events AS (
    INSERT INTO ws_events(seq,type,payload)
    SELECT clock.seq-(SELECT count(*) FROM changed)+row_number() OVER (ORDER BY changed.id),
        'account_status_changed',jsonb_build_object('accountId',changed.id,'from',changed.previous_status,'to','disconnected')
    FROM changed CROSS JOIN clock RETURNING seq
), logs AS (
    INSERT INTO account_logs(account_id,actor,action,result)
    SELECT id,'system','upgrade_disconnect','success' FROM changed RETURNING id
)
INSERT INTO gateway_actions(id,account_id,action)
SELECT 'workspace-upgrade:'||id,id,'disconnect' FROM changed;
