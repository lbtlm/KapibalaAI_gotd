UPDATE accounts
SET last_check_error=CASE status
    WHEN 'session_expired' THEN 'SESSION_EXPIRED'
    WHEN 'suspended' THEN 'ACCOUNT_SUSPENDED'
END
WHERE status IN ('session_expired','suspended')
    AND last_check_result='failed'
    AND last_check_error IN ('ILLEGAL_TRANSITION','ACCOUNT_OFFLINE');
