-- Default reference data the app expects to exist (no personal data).
INSERT INTO settings (key, value) VALUES
    ('fixed_monthly_deposit_default_pln', '400');

-- One cash snapshot per portfolio; balances are entered in the app.
INSERT INTO account_cash_snapshot (portfolio, balance_pln, update_reason) VALUES
    ('VARIABLE', 0, 'MANUAL'),
    ('FIXED',    0, 'MANUAL');
