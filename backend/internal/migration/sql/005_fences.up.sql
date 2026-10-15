ALTER TABLE agent_runs ADD COLUMN execution_epoch bigint NOT NULL DEFAULT 0;
ALTER TABLE agent_runs ADD COLUMN budget_checkpoint timestamptz NOT NULL DEFAULT now();
CREATE TABLE runtime_clock(id integer PRIMARY KEY CHECK(id=1),last_active timestamptz NOT NULL DEFAULT now());
INSERT INTO runtime_clock(id) VALUES(1);
