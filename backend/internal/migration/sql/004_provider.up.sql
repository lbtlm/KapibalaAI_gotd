CREATE TABLE provider_turns(ordinal bigserial PRIMARY KEY,run_id text NOT NULL,request_hash text NOT NULL,response text NOT NULL,UNIQUE(run_id,request_hash));
CREATE TABLE provider_tool_items(run_id text NOT NULL,tool_id text NOT NULL,assistant jsonb,items jsonb,PRIMARY KEY(run_id,tool_id));
CREATE TABLE provider_script(id integer PRIMARY KEY CHECK(id=1),plan jsonb NOT NULL DEFAULT '{}');
INSERT INTO provider_script(id) VALUES(1);
CREATE TABLE provider_audits(id bigserial PRIMARY KEY,request jsonb NOT NULL,response text NOT NULL);
