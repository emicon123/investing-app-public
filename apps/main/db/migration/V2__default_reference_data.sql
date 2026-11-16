-- Default reference data the app expects to exist (no personal data).
-- IKE / IKZE annual contribution limits are the public statutory limits for 2026.
INSERT INTO settings (key, value) VALUES
    ('ike_annual_limit_pln',                '23472.00'),
    ('ikze_annual_limit_pln',               '9388.00'),
    ('retirement.monthly_contribution_pln', '1000'),
    ('rebalancing.macro_derisk_enabled',    'true');

-- Singleton row controlling the trading bot; disabled by default.
INSERT INTO bot_control (id, trading_enabled) VALUES (1, FALSE);
