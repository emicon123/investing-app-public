-- Default reference data the app expects to exist (no personal data).
-- IKE / IKZE annual contribution limits are the public statutory limits for 2026.
INSERT INTO settings (key, value) VALUES
    ('monthly_contribution_pln', '4000'),
    ('ike_annual_limit_pln',     '23472'),
    ('ikze_annual_limit_pln',    '11836.80');

-- Singleton cash snapshot; balances are entered in the app.
INSERT INTO account_cash_snapshot (id) VALUES (1);
