-- Retain deletion audits; validate only future writes instead of removing history.
ALTER TABLE account_logs ADD CONSTRAINT account_logs_account_id_fkey
    FOREIGN KEY(account_id) REFERENCES accounts(id) NOT VALID;
