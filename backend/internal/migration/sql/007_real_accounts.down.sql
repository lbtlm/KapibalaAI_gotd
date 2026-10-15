ALTER TABLE accounts DROP CONSTRAINT accounts_verification_source_check;
ALTER TABLE accounts ADD CONSTRAINT accounts_verification_source_check CHECK(verification_source IN ('telegram','simulated'));
-- Historical evidence retirement is not reversed without the pre-upgrade backup.
-- Keep last_synced_at nullable so retained unverified caches survive downgrade.
