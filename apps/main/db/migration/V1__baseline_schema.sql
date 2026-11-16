-- Baseline schema of the main app (squashed).
--
-- The private development history had ~200 incremental migrations, many of which loaded or
-- reconciled real portfolio data. For this public copy they were squashed into one schema-only
-- baseline, generated with `pg_dump --schema-only` after applying the full history to an empty
-- PostgreSQL 17 database. No rows are included here; see V2 for default reference data.

--
-- PostgreSQL database dump
--

-- Dumped from database version 17.11
-- Dumped by pg_dump version 17.11

--
-- Name: account_type_enum; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.account_type_enum AS ENUM (
    'IKE',
    'IKZE',
    'REGULAR'
);

--
-- Name: asset_class_enum; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.asset_class_enum AS ENUM (
    'STOCK',
    'CRYPTO',
    'ETF',
    'CURRENCY'
);

--
-- Name: capital_source_enum; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.capital_source_enum AS ENUM (
    'EXTERNAL',
    'INTERNAL_TRANSFER'
);

--
-- Name: currency_enum; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.currency_enum AS ENUM (
    'PLN',
    'EUR',
    'USD',
    'CHF',
    'GBP'
);

--
-- Name: exchange_enum; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.exchange_enum AS ENUM (
    'XTB',
    'BINANCE'
);

--
-- Name: exposure_enum; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.exposure_enum AS ENUM (
    'CRYPTO',
    'METALS',
    'CASH',
    'QUANTUM',
    'BONDS',
    'EXPERIMENTS',
    'EQUITY'
);

--
-- Name: portfolio_enum; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.portfolio_enum AS ENUM (
    'MAIN',
    'RETIREMENT'
);

--
-- Name: portfolios_included_enum; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.portfolios_included_enum AS ENUM (
    'BOTH',
    'MAIN',
    'RETIREMENT'
);

--
-- Name: session_status_enum; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.session_status_enum AS ENUM (
    'PLANNED',
    'DONE'
);

--
-- Name: trade_side_enum; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.trade_side_enum AS ENUM (
    'BUY',
    'SELL'
);

--
-- Name: trade_type_enum; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.trade_type_enum AS ENUM (
    'DCA_BUY',
    'MIGRATION_BUY',
    'MIGRATION_SELL',
    'REBALANCING_TRIM_SELL',
    'REBALANCING_REINVEST_BUY',
    'SEED_IMPORT'
);

--
-- Name: set_updated_at(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.set_updated_at() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$;

--
-- Name: account_cap_adjustment; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.account_cap_adjustment (
    id bigint NOT NULL,
    account_type public.account_type_enum NOT NULL,
    year integer NOT NULL,
    adjustment_pln numeric(12,2) DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT account_cap_adjustment_type_check CHECK ((account_type = ANY (ARRAY['IKE'::public.account_type_enum, 'IKZE'::public.account_type_enum]))),
    CONSTRAINT account_cap_adjustment_year_range CHECK (((year >= 2021) AND (year <= 2100)))
);

--
-- Name: account_cap_adjustment_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.account_cap_adjustment_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: account_cap_adjustment_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.account_cap_adjustment_id_seq OWNED BY public.account_cap_adjustment.id;

--
-- Name: asset_news; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.asset_news (
    id bigint NOT NULL,
    ticker character varying(20) NOT NULL,
    asset_name character varying(200) NOT NULL,
    news_date date NOT NULL,
    headline character varying(300) NOT NULL,
    summary text NOT NULL,
    sentiment character varying(10),
    sources jsonb DEFAULT '[]'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT asset_news_sentiment_check CHECK (((sentiment)::text = ANY ((ARRAY['POSITIVE'::character varying, 'NEUTRAL'::character varying, 'NEGATIVE'::character varying])::text[])))
);

--
-- Name: asset_news_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.asset_news_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: asset_news_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.asset_news_id_seq OWNED BY public.asset_news.id;

--
-- Name: asset_purchases; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.asset_purchases (
    id bigint NOT NULL,
    session_id bigint,
    portfolio public.portfolio_enum NOT NULL,
    ticker character varying(20) NOT NULL,
    account_type public.account_type_enum NOT NULL,
    units_delta numeric(18,8) NOT NULL,
    price_pln numeric(18,8) NOT NULL,
    total_pln numeric(18,4) NOT NULL,
    purchased_at timestamp with time zone DEFAULT now() NOT NULL,
    trade_type public.trade_type_enum NOT NULL,
    xtb_import_key text,
    CONSTRAINT ap_price_positive CHECK ((price_pln > (0)::numeric))
);

--
-- Name: asset_purchases_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.asset_purchases_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: asset_purchases_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.asset_purchases_id_seq OWNED BY public.asset_purchases.id;

--
-- Name: assets; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.assets (
    id bigint NOT NULL,
    portfolio public.portfolio_enum NOT NULL,
    key text NOT NULL,
    name character varying(200) NOT NULL,
    exposure public.exposure_enum NOT NULL,
    asset_class public.asset_class_enum NOT NULL,
    target_allocation_pct numeric(7,4),
    target_allocation_pct_risk_off numeric(7,4),
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    CONSTRAINT assets_target_pct_range CHECK (((target_allocation_pct IS NULL) OR ((target_allocation_pct >= (0)::numeric) AND (target_allocation_pct <= (100)::numeric)))),
    CONSTRAINT assets_target_pct_risk_off_range CHECK (((target_allocation_pct_risk_off IS NULL) OR ((target_allocation_pct_risk_off >= (0)::numeric) AND (target_allocation_pct_risk_off <= (100)::numeric))))
);

--
-- Name: assets_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.assets_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: assets_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.assets_id_seq OWNED BY public.assets.id;

--
-- Name: binance_transfers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binance_transfers (
    id bigint NOT NULL,
    transfer_date date NOT NULL,
    amount_pln numeric(14,2) NOT NULL,
    amount_native numeric(18,4),
    native_currency public.currency_enum,
    comment text,
    source character varying(10) DEFAULT 'MANUAL'::character varying NOT NULL,
    session_id bigint,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT binance_transfers_amount_pln_check CHECK ((amount_pln > (0)::numeric)),
    CONSTRAINT binance_transfers_source_check CHECK (((source)::text = ANY ((ARRAY['MANUAL'::character varying, 'SESSION'::character varying])::text[]))),
    CONSTRAINT bt_native_pair_chk CHECK (((amount_native IS NULL) = (native_currency IS NULL)))
);

--
-- Name: binance_transfers_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.binance_transfers_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: binance_transfers_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.binance_transfers_id_seq OWNED BY public.binance_transfers.id;

--
-- Name: bot_calendar_event; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.bot_calendar_event (
    id bigint NOT NULL,
    event_time timestamp with time zone NOT NULL,
    currency character varying(10) NOT NULL,
    country character varying(10),
    event_name character varying(150) NOT NULL,
    impact character varying(10) NOT NULL,
    forecast character varying(50),
    previous character varying(50),
    actual character varying(50),
    source character varying(20) DEFAULT 'FAIRECONOMY'::character varying NOT NULL,
    external_id character varying(100) NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    actual_source character varying(20),
    CONSTRAINT bot_calendar_event_impact_check CHECK (((impact)::text = ANY ((ARRAY['HIGH'::character varying, 'MEDIUM'::character varying, 'LOW'::character varying, 'HOLIDAY'::character varying])::text[])))
);

--
-- Name: bot_calendar_event_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.bot_calendar_event_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: bot_calendar_event_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.bot_calendar_event_id_seq OWNED BY public.bot_calendar_event.id;

--
-- Name: bot_control; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.bot_control (
    id smallint DEFAULT 1 NOT NULL,
    trading_enabled boolean DEFAULT true NOT NULL,
    paused_reason character varying(255),
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT bot_control_id_check CHECK ((id = 1))
);

--
-- Name: bot_daily_plan; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.bot_daily_plan (
    id bigint NOT NULL,
    plan_date date NOT NULL,
    status character varying(20) DEFAULT 'PUBLISHED'::character varying NOT NULL,
    summary text,
    market_context jsonb DEFAULT '{}'::jsonb NOT NULL,
    model character varying(50),
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT bot_daily_plan_status_check CHECK (((status)::text = ANY ((ARRAY['PUBLISHED'::character varying, 'EMPTY'::character varying])::text[])))
);

--
-- Name: bot_daily_plan_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.bot_daily_plan_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: bot_daily_plan_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.bot_daily_plan_id_seq OWNED BY public.bot_daily_plan.id;

--
-- Name: bot_plan_scenario; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.bot_plan_scenario (
    id bigint NOT NULL,
    plan_id bigint NOT NULL,
    calendar_event_id bigint,
    event_name character varying(150) NOT NULL,
    event_time timestamp with time zone NOT NULL,
    scenario_label character varying(50) NOT NULL,
    trigger_condition text NOT NULL,
    action character varying(10) NOT NULL,
    leverage smallint,
    reasoning text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    surprise_min numeric(18,4),
    surprise_max numeric(18,4),
    confidence numeric(3,2),
    CONSTRAINT bot_plan_scenario_action_check CHECK (((action)::text = ANY ((ARRAY['LONG'::character varying, 'SHORT'::character varying, 'HOLD'::character varying])::text[]))),
    CONSTRAINT bot_plan_scenario_confidence_check CHECK (((confidence >= (0)::numeric) AND (confidence <= (1)::numeric))),
    CONSTRAINT bot_plan_scenario_leverage_check CHECK (((leverage >= 1) AND (leverage <= 10)))
);

--
-- Name: bot_plan_scenario_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.bot_plan_scenario_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: bot_plan_scenario_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.bot_plan_scenario_id_seq OWNED BY public.bot_plan_scenario.id;

--
-- Name: bot_signal; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.bot_signal (
    id bigint NOT NULL,
    calendar_event_id bigint,
    plan_scenario_id bigint,
    event_name character varying(150) NOT NULL,
    actual character varying(50) NOT NULL,
    forecast character varying(50),
    previous character varying(50),
    surprise_desc text,
    decision character varying(10) NOT NULL,
    executed boolean DEFAULT false NOT NULL,
    raw_payload jsonb,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT bot_signal_decision_check CHECK (((decision)::text = ANY ((ARRAY['LONG'::character varying, 'SHORT'::character varying, 'HOLD'::character varying])::text[])))
);

--
-- Name: bot_signal_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.bot_signal_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: bot_signal_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.bot_signal_id_seq OWNED BY public.bot_signal.id;

--
-- Name: bot_trade; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.bot_trade (
    id bigint NOT NULL,
    signal_id bigint,
    calendar_event_id bigint,
    side character varying(10) NOT NULL,
    status character varying(10) DEFAULT 'OPEN'::character varying NOT NULL,
    quantity numeric(18,8) NOT NULL,
    leverage smallint DEFAULT 1 NOT NULL,
    entry_price numeric(18,2) NOT NULL,
    exit_price numeric(18,2),
    pnl_pln numeric(18,4),
    pnl_pct numeric(7,4),
    okx_order_id character varying(64),
    notes text,
    opened_at timestamp with time zone DEFAULT now() NOT NULL,
    closed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    okx_algo_id character varying(64),
    margin_allocated_pln numeric(18,4),
    sizing_confidence numeric(3,2),
    scalp_session_status character varying(16) DEFAULT 'NONE'::character varying NOT NULL,
    scalp_session_claimed_at timestamp with time zone,
    CONSTRAINT bot_trade_entry_price_check CHECK ((entry_price > (0)::numeric)),
    CONSTRAINT bot_trade_exit_price_check CHECK ((exit_price > (0)::numeric)),
    CONSTRAINT bot_trade_leverage_check CHECK (((leverage >= 1) AND (leverage <= 10))),
    CONSTRAINT bot_trade_quantity_check CHECK ((quantity > (0)::numeric)),
    CONSTRAINT bot_trade_scalp_session_status_check CHECK (((scalp_session_status)::text = ANY ((ARRAY['NONE'::character varying, 'PENDING'::character varying, 'RUNNING'::character varying, 'DONE'::character varying])::text[]))),
    CONSTRAINT bot_trade_side_check CHECK (((side)::text = ANY ((ARRAY['LONG'::character varying, 'SHORT'::character varying])::text[]))),
    CONSTRAINT bot_trade_status_check CHECK (((status)::text = ANY ((ARRAY['OPEN'::character varying, 'CLOSED'::character varying])::text[])))
);

--
-- Name: bot_trade_action; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.bot_trade_action (
    id bigint NOT NULL,
    trade_id bigint NOT NULL,
    action_type character varying(32) NOT NULL,
    price numeric(18,2),
    quantity numeric(18,8),
    reasoning text,
    source character varying(20) DEFAULT 'BOT'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT bot_trade_action_action_type_check CHECK (((action_type)::text = ANY ((ARRAY['SET_SL'::character varying, 'SET_TP'::character varying, 'TRAIL_SL'::character varying, 'PARTIAL_CLOSE'::character varying, 'FULL_CLOSE'::character varying, 'SCALP_SESSION_STARTED'::character varying, 'SCALP_SESSION_FINISHED'::character varying])::text[]))),
    CONSTRAINT bot_trade_action_source_check CHECK (((source)::text = ANY ((ARRAY['BOT'::character varying, 'CLAUDE'::character varying, 'SYSTEM'::character varying])::text[])))
);

--
-- Name: bot_trade_action_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.bot_trade_action_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: bot_trade_action_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.bot_trade_action_id_seq OWNED BY public.bot_trade_action.id;

--
-- Name: bot_trade_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.bot_trade_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: bot_trade_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.bot_trade_id_seq OWNED BY public.bot_trade.id;

--
-- Name: bot_trade_tick; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.bot_trade_tick (
    id bigint NOT NULL,
    trade_id bigint NOT NULL,
    price numeric(18,2) NOT NULL,
    volume numeric(18,8),
    cvd numeric(18,8),
    atr_1m numeric(18,8),
    ob_imbalance numeric(7,4),
    funding_rate numeric(10,6),
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    long_short_ratio numeric(7,4),
    open_interest numeric(18,2)
);

--
-- Name: bot_trade_tick_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.bot_trade_tick_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: bot_trade_tick_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.bot_trade_tick_id_seq OWNED BY public.bot_trade_tick.id;

--
-- Name: cash_positions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.cash_positions (
    id bigint NOT NULL,
    location character varying(100) NOT NULL,
    amount_pln numeric(18,4) NOT NULL,
    recorded_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT cash_positions_amount_nonneg CHECK ((amount_pln >= (0)::numeric))
);

--
-- Name: cash_positions_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.cash_positions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: cash_positions_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.cash_positions_id_seq OWNED BY public.cash_positions.id;

--
-- Name: crypto_trades; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.crypto_trades (
    id bigint NOT NULL,
    trade_date date NOT NULL,
    ticker character varying(20) NOT NULL,
    exchange public.exchange_enum NOT NULL,
    account_type public.account_type_enum NOT NULL,
    side public.trade_side_enum NOT NULL,
    quantity numeric(18,8) NOT NULL,
    unit_price numeric(18,8) NOT NULL,
    price_currency public.currency_enum NOT NULL,
    total_amount numeric(18,4) NOT NULL,
    amount_pln numeric(18,4) NOT NULL,
    source character varying(10) DEFAULT 'MANUAL'::character varying NOT NULL,
    notes text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT crypto_trades_price_positive CHECK ((unit_price > (0)::numeric)),
    CONSTRAINT crypto_trades_qty_positive CHECK ((quantity > (0)::numeric)),
    CONSTRAINT crypto_trades_source_check CHECK (((source)::text = ANY ((ARRAY['MIGRATION'::character varying, 'MANUAL'::character varying])::text[]))),
    CONSTRAINT crypto_trades_total_amount_positive CHECK ((total_amount > (0)::numeric))
);

--
-- Name: crypto_trades_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.crypto_trades_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: crypto_trades_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.crypto_trades_id_seq OWNED BY public.crypto_trades.id;

--
-- Name: deposits; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.deposits (
    id bigint NOT NULL,
    deposit_date date NOT NULL,
    amount numeric(18,4) NOT NULL,
    currency public.currency_enum NOT NULL,
    exchange public.exchange_enum NOT NULL,
    account_type public.account_type_enum NOT NULL,
    portfolio public.portfolio_enum NOT NULL,
    comment text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    capital_source public.capital_source_enum DEFAULT 'EXTERNAL'::public.capital_source_enum NOT NULL,
    CONSTRAINT deposits_amount_positive CHECK ((amount > (0)::numeric))
);

--
-- Name: deposits_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.deposits_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: deposits_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.deposits_id_seq OWNED BY public.deposits.id;

--
-- Name: fx_rate_cache; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.fx_rate_cache (
    id bigint NOT NULL,
    from_currency public.currency_enum NOT NULL,
    rate_to_pln numeric(18,8) NOT NULL,
    source character varying(20) NOT NULL,
    fetched_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT fx_rate_cache_source_check CHECK (((source)::text = ANY ((ARRAY['YAHOO_FINANCE'::character varying, 'NBP_API'::character varying])::text[]))),
    CONSTRAINT fx_rate_positive CHECK ((rate_to_pln > (0)::numeric))
);

--
-- Name: fx_rate_cache_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.fx_rate_cache_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: fx_rate_cache_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.fx_rate_cache_id_seq OWNED BY public.fx_rate_cache.id;

--
-- Name: historia; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.historia (
    id bigint NOT NULL,
    snapshot_date date NOT NULL,
    portfolio public.portfolio_enum NOT NULL,
    total_deposits numeric(15,2) NOT NULL,
    portfolio_value numeric(15,2) NOT NULL,
    xirr numeric(8,6),
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    position_values jsonb,
    CONSTRAINT historia_deposits_nonneg CHECK ((total_deposits >= (0)::numeric)),
    CONSTRAINT historia_value_nonneg CHECK ((portfolio_value >= (0)::numeric))
);

--
-- Name: historia_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.historia_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: historia_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.historia_id_seq OWNED BY public.historia.id;

--
-- Name: idempotency_keys; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.idempotency_keys (
    id bigint NOT NULL,
    idem_key character varying(255) NOT NULL,
    request_hash character(64) NOT NULL,
    status character varying(12) DEFAULT 'IN_PROGRESS'::character varying NOT NULL,
    response_code smallint,
    content_type character varying(100),
    response_body bytea,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    completed_at timestamp with time zone,
    CONSTRAINT idempotency_keys_code_ck CHECK (((response_code IS NULL) OR ((response_code >= 200) AND (response_code <= 299)))),
    CONSTRAINT idempotency_keys_completed_ck CHECK ((((status)::text = 'COMPLETED'::text) = ((response_code IS NOT NULL) AND (completed_at IS NOT NULL)))),
    CONSTRAINT idempotency_keys_status_ck CHECK (((status)::text = ANY ((ARRAY['IN_PROGRESS'::character varying, 'COMPLETED'::character varying])::text[])))
);

--
-- Name: TABLE idempotency_keys; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON TABLE public.idempotency_keys IS 'ADR-111: Idempotency-Key records for money-moving POSTs (24 h retention, purged by the middleware).';

--
-- Name: idempotency_keys_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.idempotency_keys_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: idempotency_keys_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.idempotency_keys_id_seq OWNED BY public.idempotency_keys.id;

--
-- Name: inflation; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.inflation (
    id bigint NOT NULL,
    year integer NOT NULL,
    month integer NOT NULL,
    cpi_value numeric(7,4) NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT inflation_month_range CHECK (((month >= 1) AND (month <= 12))),
    CONSTRAINT inflation_year_range CHECK (((year >= 2000) AND (year <= 2100)))
);

--
-- Name: inflation_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.inflation_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: inflation_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.inflation_id_seq OWNED BY public.inflation.id;

--
-- Name: macro_observations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.macro_observations (
    id bigint NOT NULL,
    series_code character varying(20) NOT NULL,
    source character varying(10) NOT NULL,
    observation_date date NOT NULL,
    value numeric(20,4) NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT macro_observations_source_check CHECK (((source)::text = ANY ((ARRAY['FRED'::character varying, 'ECB'::character varying, 'BOE'::character varying, 'BOJ'::character varying, 'CHINADATA'::character varying, 'TRADINGEC'::character varying, 'PBC'::character varying, 'YCHARTS'::character varying, 'MANUAL'::character varying, 'DERIVED'::character varying])::text[])))
);

--
-- Name: macro_observations_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.macro_observations_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: macro_observations_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.macro_observations_id_seq OWNED BY public.macro_observations.id;

--
-- Name: macro_state; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.macro_state (
    id bigint NOT NULL,
    as_of_date date NOT NULL,
    risk_off boolean NOT NULL,
    net_liq_latest_usd_mln numeric(20,4),
    net_liq_yoy numeric(18,8),
    net_liq_yoy_accel numeric(18,8),
    net_liq_risk_off boolean,
    global_m2_latest_usd_mln numeric(20,4),
    global_m2_yoy numeric(18,8),
    global_m2_yoy_accel numeric(18,8),
    global_m2_risk_off boolean,
    global_m2_stale boolean DEFAULT false NOT NULL,
    computed_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    blend_factor numeric(7,6),
    composite_z numeric(18,8),
    smoothed_z numeric(18,8),
    CONSTRAINT macro_state_blend_factor_range CHECK (((blend_factor IS NULL) OR ((blend_factor >= (0)::numeric) AND (blend_factor <= (1)::numeric))))
);

--
-- Name: COLUMN macro_state.blend_factor; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON COLUMN public.macro_state.blend_factor IS 'ADR-044: clip(0.5 + smoothed_z / 0.5, 0, 1) (ADR-071 orientation). 0 = full risk-off, 1 = full risk-on. Drives per-position target interpolation. NULL while there is insufficient history to compute a smoothed z-score (bootstrap period).';

--
-- Name: COLUMN macro_state.composite_z; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON COLUMN public.macro_state.composite_z IS 'ADR-044: 0.7 * zscore(net-liq YoY accel) + 0.3 * zscore(Global-M2 YoY accel), each an expanding, no-lookahead z-score vs. its own history. Persisted for debuggability, same spirit as net_liq_yoy_accel.';

--
-- Name: COLUMN macro_state.smoothed_z; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON COLUMN public.macro_state.smoothed_z IS 'ADR-044: 3-month rolling average of composite_z. Input to blend_factor.';

--
-- Name: macro_state_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.macro_state_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: macro_state_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.macro_state_id_seq OWNED BY public.macro_state.id;

--
-- Name: positions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.positions (
    id bigint NOT NULL,
    ticker character varying(20) NOT NULL,
    name character varying(200) NOT NULL,
    portfolio public.portfolio_enum NOT NULL,
    exchange public.exchange_enum NOT NULL,
    asset_class public.asset_class_enum NOT NULL,
    price_currency public.currency_enum NOT NULL,
    units_ike numeric(18,8) DEFAULT 0 NOT NULL,
    units_ikze numeric(18,8) DEFAULT 0 NOT NULL,
    units_regular_eur numeric(18,8) DEFAULT 0 NOT NULL,
    units_regular_usd numeric(18,8) DEFAULT 0 NOT NULL,
    units_regular_pln numeric(18,8) DEFAULT 0 NOT NULL,
    total_spent_pln numeric(18,4),
    avg_buy_price numeric(18,8),
    yahoo_ticker character varying(50),
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    purchase_currency public.currency_enum DEFAULT 'PLN'::public.currency_enum NOT NULL,
    exposure public.exposure_enum NOT NULL,
    company_backed boolean DEFAULT true NOT NULL,
    asset_id bigint NOT NULL,
    is_primary boolean DEFAULT false NOT NULL,
    deleted_at timestamp with time zone,
    CONSTRAINT positions_units_eur_nonneg CHECK ((units_regular_eur >= (0)::numeric)),
    CONSTRAINT positions_units_ike_nonneg CHECK ((units_ike >= (0)::numeric)),
    CONSTRAINT positions_units_ikze_nonneg CHECK ((units_ikze >= (0)::numeric)),
    CONSTRAINT positions_units_pln_nonneg CHECK ((units_regular_pln >= (0)::numeric)),
    CONSTRAINT positions_units_usd_nonneg CHECK ((units_regular_usd >= (0)::numeric))
);

--
-- Name: positions_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.positions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: positions_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.positions_id_seq OWNED BY public.positions.id;

--
-- Name: price_cache; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.price_cache (
    id bigint NOT NULL,
    ticker character varying(50) NOT NULL,
    price numeric(18,8) NOT NULL,
    currency public.currency_enum NOT NULL,
    price_pln numeric(18,8) NOT NULL,
    fetched_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: price_cache_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.price_cache_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: price_cache_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.price_cache_id_seq OWNED BY public.price_cache.id;

--
-- Name: purchase_session_items; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.purchase_session_items (
    id bigint NOT NULL,
    session_id bigint NOT NULL,
    portfolio public.portfolio_enum NOT NULL,
    ticker character varying(20) NOT NULL,
    account_type public.account_type_enum NOT NULL,
    planned_units numeric(18,8),
    planned_value_pln numeric(18,4),
    actual_units numeric(18,8),
    actual_value_pln numeric(18,4),
    price_pln_at_plan numeric(18,8),
    is_migration_sell boolean DEFAULT false NOT NULL,
    is_migration_buy boolean DEFAULT false NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT psi_migration_flags_exclusive CHECK ((NOT (is_migration_sell AND is_migration_buy))),
    CONSTRAINT psi_planned_units_nonneg CHECK (((planned_units IS NULL) OR (planned_units >= (0)::numeric))),
    CONSTRAINT psi_planned_value_nonneg CHECK (((planned_value_pln IS NULL) OR (planned_value_pln >= (0)::numeric))),
    CONSTRAINT psi_price_nonneg CHECK (((price_pln_at_plan IS NULL) OR (price_pln_at_plan > (0)::numeric)))
);

--
-- Name: purchase_session_items_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.purchase_session_items_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: purchase_session_items_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.purchase_session_items_id_seq OWNED BY public.purchase_session_items.id;

--
-- Name: purchase_sessions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.purchase_sessions (
    id bigint NOT NULL,
    status public.session_status_enum DEFAULT 'PLANNED'::public.session_status_enum NOT NULL,
    planned_at timestamp with time zone DEFAULT now() NOT NULL,
    done_at timestamp with time zone,
    notes text,
    retirement_budget_pln numeric(18,4),
    main_budget_pln numeric(18,4),
    portfolios_included public.portfolios_included_enum DEFAULT 'BOTH'::public.portfolios_included_enum NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT ps_done_at_consistency CHECK ((((status = 'PLANNED'::public.session_status_enum) AND (done_at IS NULL)) OR ((status = 'DONE'::public.session_status_enum) AND (done_at IS NOT NULL)))),
    CONSTRAINT ps_emerytura_budget_nonneg CHECK (((retirement_budget_pln IS NULL) OR (retirement_budget_pln >= (0)::numeric))),
    CONSTRAINT ps_inwestycje_budget_nonneg CHECK (((main_budget_pln IS NULL) OR (main_budget_pln >= (0)::numeric)))
);

--
-- Name: purchase_sessions_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.purchase_sessions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: purchase_sessions_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.purchase_sessions_id_seq OWNED BY public.purchase_sessions.id;

--
-- Name: push_subscriptions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.push_subscriptions (
    id bigint NOT NULL,
    user_email character varying(320) NOT NULL,
    endpoint text NOT NULL,
    p256dh character varying(128) NOT NULL,
    auth character varying(64) NOT NULL,
    device_label character varying(100) NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    last_success_at timestamp with time zone,
    CONSTRAINT push_subscriptions_endpoint_https CHECK (((endpoint ~~ 'https://%'::text) AND (char_length(endpoint) <= 2048))),
    CONSTRAINT push_subscriptions_keys_not_blank CHECK ((((p256dh)::text <> ''::text) AND ((auth)::text <> ''::text))),
    CONSTRAINT push_subscriptions_user_email_not_blank CHECK ((btrim((user_email)::text) <> ''::text))
);

--
-- Name: push_subscriptions_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.push_subscriptions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: push_subscriptions_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.push_subscriptions_id_seq OWNED BY public.push_subscriptions.id;

--
-- Name: settings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.settings (
    key character varying(100) NOT NULL,
    value text NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: tax_returns; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.tax_returns (
    id bigint NOT NULL,
    year integer NOT NULL,
    gross_gain numeric(18,4) NOT NULL,
    tax_paid numeric(18,4) NOT NULL,
    effective_rate_pct numeric(7,4) NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT tax_returns_rate_range CHECK (((effective_rate_pct >= (0)::numeric) AND (effective_rate_pct <= (100)::numeric))),
    CONSTRAINT tax_returns_tax_nonneg CHECK ((tax_paid >= (0)::numeric)),
    CONSTRAINT tax_returns_year_range CHECK (((year >= 2021) AND (year <= 2100)))
);

--
-- Name: tax_returns_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.tax_returns_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: tax_returns_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.tax_returns_id_seq OWNED BY public.tax_returns.id;

--
-- Name: account_cap_adjustment id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.account_cap_adjustment ALTER COLUMN id SET DEFAULT nextval('public.account_cap_adjustment_id_seq'::regclass);

--
-- Name: asset_news id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.asset_news ALTER COLUMN id SET DEFAULT nextval('public.asset_news_id_seq'::regclass);

--
-- Name: asset_purchases id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.asset_purchases ALTER COLUMN id SET DEFAULT nextval('public.asset_purchases_id_seq'::regclass);

--
-- Name: assets id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.assets ALTER COLUMN id SET DEFAULT nextval('public.assets_id_seq'::regclass);

--
-- Name: binance_transfers id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binance_transfers ALTER COLUMN id SET DEFAULT nextval('public.binance_transfers_id_seq'::regclass);

--
-- Name: bot_calendar_event id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_calendar_event ALTER COLUMN id SET DEFAULT nextval('public.bot_calendar_event_id_seq'::regclass);

--
-- Name: bot_daily_plan id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_daily_plan ALTER COLUMN id SET DEFAULT nextval('public.bot_daily_plan_id_seq'::regclass);

--
-- Name: bot_plan_scenario id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_plan_scenario ALTER COLUMN id SET DEFAULT nextval('public.bot_plan_scenario_id_seq'::regclass);

--
-- Name: bot_signal id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_signal ALTER COLUMN id SET DEFAULT nextval('public.bot_signal_id_seq'::regclass);

--
-- Name: bot_trade id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_trade ALTER COLUMN id SET DEFAULT nextval('public.bot_trade_id_seq'::regclass);

--
-- Name: bot_trade_action id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_trade_action ALTER COLUMN id SET DEFAULT nextval('public.bot_trade_action_id_seq'::regclass);

--
-- Name: bot_trade_tick id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_trade_tick ALTER COLUMN id SET DEFAULT nextval('public.bot_trade_tick_id_seq'::regclass);

--
-- Name: cash_positions id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cash_positions ALTER COLUMN id SET DEFAULT nextval('public.cash_positions_id_seq'::regclass);

--
-- Name: crypto_trades id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.crypto_trades ALTER COLUMN id SET DEFAULT nextval('public.crypto_trades_id_seq'::regclass);

--
-- Name: deposits id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.deposits ALTER COLUMN id SET DEFAULT nextval('public.deposits_id_seq'::regclass);

--
-- Name: fx_rate_cache id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.fx_rate_cache ALTER COLUMN id SET DEFAULT nextval('public.fx_rate_cache_id_seq'::regclass);

--
-- Name: historia id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.historia ALTER COLUMN id SET DEFAULT nextval('public.historia_id_seq'::regclass);

--
-- Name: idempotency_keys id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.idempotency_keys ALTER COLUMN id SET DEFAULT nextval('public.idempotency_keys_id_seq'::regclass);

--
-- Name: inflation id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inflation ALTER COLUMN id SET DEFAULT nextval('public.inflation_id_seq'::regclass);

--
-- Name: macro_observations id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.macro_observations ALTER COLUMN id SET DEFAULT nextval('public.macro_observations_id_seq'::regclass);

--
-- Name: macro_state id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.macro_state ALTER COLUMN id SET DEFAULT nextval('public.macro_state_id_seq'::regclass);

--
-- Name: positions id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.positions ALTER COLUMN id SET DEFAULT nextval('public.positions_id_seq'::regclass);

--
-- Name: price_cache id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.price_cache ALTER COLUMN id SET DEFAULT nextval('public.price_cache_id_seq'::regclass);

--
-- Name: purchase_session_items id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.purchase_session_items ALTER COLUMN id SET DEFAULT nextval('public.purchase_session_items_id_seq'::regclass);

--
-- Name: purchase_sessions id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.purchase_sessions ALTER COLUMN id SET DEFAULT nextval('public.purchase_sessions_id_seq'::regclass);

--
-- Name: push_subscriptions id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.push_subscriptions ALTER COLUMN id SET DEFAULT nextval('public.push_subscriptions_id_seq'::regclass);

--
-- Name: tax_returns id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tax_returns ALTER COLUMN id SET DEFAULT nextval('public.tax_returns_id_seq'::regclass);

--
-- Name: account_cap_adjustment account_cap_adjustment_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.account_cap_adjustment
    ADD CONSTRAINT account_cap_adjustment_pkey PRIMARY KEY (id);

--
-- Name: asset_news asset_news_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.asset_news
    ADD CONSTRAINT asset_news_pkey PRIMARY KEY (id);

--
-- Name: asset_purchases asset_purchases_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.asset_purchases
    ADD CONSTRAINT asset_purchases_pkey PRIMARY KEY (id);

--
-- Name: assets assets_key_portfolio_uq; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.assets
    ADD CONSTRAINT assets_key_portfolio_uq UNIQUE (key, portfolio);

--
-- Name: assets assets_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.assets
    ADD CONSTRAINT assets_pkey PRIMARY KEY (id);

--
-- Name: binance_transfers binance_transfers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binance_transfers
    ADD CONSTRAINT binance_transfers_pkey PRIMARY KEY (id);

--
-- Name: bot_calendar_event bot_calendar_event_external_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_calendar_event
    ADD CONSTRAINT bot_calendar_event_external_id_key UNIQUE (external_id);

--
-- Name: bot_calendar_event bot_calendar_event_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_calendar_event
    ADD CONSTRAINT bot_calendar_event_pkey PRIMARY KEY (id);

--
-- Name: bot_control bot_control_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_control
    ADD CONSTRAINT bot_control_pkey PRIMARY KEY (id);

--
-- Name: bot_daily_plan bot_daily_plan_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_daily_plan
    ADD CONSTRAINT bot_daily_plan_pkey PRIMARY KEY (id);

--
-- Name: bot_daily_plan bot_daily_plan_plan_date_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_daily_plan
    ADD CONSTRAINT bot_daily_plan_plan_date_key UNIQUE (plan_date);

--
-- Name: bot_plan_scenario bot_plan_scenario_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_plan_scenario
    ADD CONSTRAINT bot_plan_scenario_pkey PRIMARY KEY (id);

--
-- Name: bot_signal bot_signal_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_signal
    ADD CONSTRAINT bot_signal_pkey PRIMARY KEY (id);

--
-- Name: bot_trade_action bot_trade_action_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_trade_action
    ADD CONSTRAINT bot_trade_action_pkey PRIMARY KEY (id);

--
-- Name: bot_trade bot_trade_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_trade
    ADD CONSTRAINT bot_trade_pkey PRIMARY KEY (id);

--
-- Name: bot_trade_tick bot_trade_tick_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_trade_tick
    ADD CONSTRAINT bot_trade_tick_pkey PRIMARY KEY (id);

--
-- Name: cash_positions cash_positions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cash_positions
    ADD CONSTRAINT cash_positions_pkey PRIMARY KEY (id);

--
-- Name: crypto_trades crypto_trades_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.crypto_trades
    ADD CONSTRAINT crypto_trades_pkey PRIMARY KEY (id);

--
-- Name: deposits deposits_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.deposits
    ADD CONSTRAINT deposits_pkey PRIMARY KEY (id);

--
-- Name: fx_rate_cache fx_rate_cache_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.fx_rate_cache
    ADD CONSTRAINT fx_rate_cache_pkey PRIMARY KEY (id);

--
-- Name: historia historia_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.historia
    ADD CONSTRAINT historia_pkey PRIMARY KEY (id);

--
-- Name: historia historia_uq; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.historia
    ADD CONSTRAINT historia_uq UNIQUE (snapshot_date, portfolio);

--
-- Name: idempotency_keys idempotency_keys_key_uq; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.idempotency_keys
    ADD CONSTRAINT idempotency_keys_key_uq UNIQUE (idem_key);

--
-- Name: idempotency_keys idempotency_keys_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.idempotency_keys
    ADD CONSTRAINT idempotency_keys_pkey PRIMARY KEY (id);

--
-- Name: inflation inflation_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inflation
    ADD CONSTRAINT inflation_pkey PRIMARY KEY (id);

--
-- Name: macro_observations macro_observations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.macro_observations
    ADD CONSTRAINT macro_observations_pkey PRIMARY KEY (id);

--
-- Name: macro_state macro_state_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.macro_state
    ADD CONSTRAINT macro_state_pkey PRIMARY KEY (id);

--
-- Name: positions positions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.positions
    ADD CONSTRAINT positions_pkey PRIMARY KEY (id);

--
-- Name: price_cache price_cache_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.price_cache
    ADD CONSTRAINT price_cache_pkey PRIMARY KEY (id);

--
-- Name: purchase_session_items psi_session_portfolio_ticker_account_uq; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.purchase_session_items
    ADD CONSTRAINT psi_session_portfolio_ticker_account_uq UNIQUE (session_id, portfolio, ticker, account_type);

--
-- Name: purchase_session_items purchase_session_items_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.purchase_session_items
    ADD CONSTRAINT purchase_session_items_pkey PRIMARY KEY (id);

--
-- Name: purchase_sessions purchase_sessions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.purchase_sessions
    ADD CONSTRAINT purchase_sessions_pkey PRIMARY KEY (id);

--
-- Name: push_subscriptions push_subscriptions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.push_subscriptions
    ADD CONSTRAINT push_subscriptions_pkey PRIMARY KEY (id);

--
-- Name: settings settings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.settings
    ADD CONSTRAINT settings_pkey PRIMARY KEY (key);

--
-- Name: tax_returns tax_returns_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tax_returns
    ADD CONSTRAINT tax_returns_pkey PRIMARY KEY (id);

--
-- Name: account_cap_adjustment uq_account_cap_adjustment_type_year; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.account_cap_adjustment
    ADD CONSTRAINT uq_account_cap_adjustment_type_year UNIQUE (account_type, year);

--
-- Name: asset_news uq_asset_news_ticker_date; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.asset_news
    ADD CONSTRAINT uq_asset_news_ticker_date UNIQUE (ticker, news_date);

--
-- Name: macro_observations uq_macro_obs_series_date; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.macro_observations
    ADD CONSTRAINT uq_macro_obs_series_date UNIQUE (series_code, observation_date);

--
-- Name: macro_state uq_macro_state_as_of_date; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.macro_state
    ADD CONSTRAINT uq_macro_state_as_of_date UNIQUE (as_of_date);

--
-- Name: push_subscriptions uq_push_subscriptions_endpoint; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.push_subscriptions
    ADD CONSTRAINT uq_push_subscriptions_endpoint UNIQUE (endpoint);

--
-- Name: ap_portfolio_ticker_date_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ap_portfolio_ticker_date_idx ON public.asset_purchases USING btree (portfolio, ticker, purchased_at DESC);

--
-- Name: ap_session_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ap_session_idx ON public.asset_purchases USING btree (session_id);

--
-- Name: asset_purchases_xtb_import_key_uq; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX asset_purchases_xtb_import_key_uq ON public.asset_purchases USING btree (xtb_import_key);

--
-- Name: bt_session_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX bt_session_id_idx ON public.binance_transfers USING btree (session_id);

--
-- Name: bt_transfer_date_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX bt_transfer_date_idx ON public.binance_transfers USING btree (transfer_date);

--
-- Name: cash_positions_recorded_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX cash_positions_recorded_at_idx ON public.cash_positions USING btree (recorded_at DESC);

--
-- Name: deposits_account_type_date_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX deposits_account_type_date_idx ON public.deposits USING btree (account_type, deposit_date);

--
-- Name: deposits_portfolio_date_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX deposits_portfolio_date_idx ON public.deposits USING btree (portfolio, deposit_date);

--
-- Name: fx_rate_cache_currency_uq; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX fx_rate_cache_currency_uq ON public.fx_rate_cache USING btree (from_currency);

--
-- Name: fx_rate_cache_fetched_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX fx_rate_cache_fetched_at_idx ON public.fx_rate_cache USING btree (fetched_at DESC);

--
-- Name: idempotency_keys_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idempotency_keys_created_at_idx ON public.idempotency_keys USING btree (created_at);

--
-- Name: idx_asset_news_date; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_asset_news_date ON public.asset_news USING btree (news_date DESC);

--
-- Name: idx_asset_news_ticker_date; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_asset_news_ticker_date ON public.asset_news USING btree (ticker, news_date DESC);

--
-- Name: idx_bot_action_trade; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_bot_action_trade ON public.bot_trade_action USING btree (trade_id, created_at);

--
-- Name: idx_bot_cal_impact; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_bot_cal_impact ON public.bot_calendar_event USING btree (impact, event_time);

--
-- Name: idx_bot_cal_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_bot_cal_time ON public.bot_calendar_event USING btree (event_time);

--
-- Name: idx_bot_scenario_event_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_bot_scenario_event_time ON public.bot_plan_scenario USING btree (event_time);

--
-- Name: idx_bot_scenario_plan; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_bot_scenario_plan ON public.bot_plan_scenario USING btree (plan_id);

--
-- Name: idx_bot_signal_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_bot_signal_created ON public.bot_signal USING btree (created_at DESC);

--
-- Name: idx_bot_tick_trade_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_bot_tick_trade_time ON public.bot_trade_tick USING btree (trade_id, created_at);

--
-- Name: idx_bot_trade_opened; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_bot_trade_opened ON public.bot_trade USING btree (opened_at DESC);

--
-- Name: idx_bot_trade_scalp_pending; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_bot_trade_scalp_pending ON public.bot_trade USING btree (id) WHERE ((scalp_session_status)::text = 'PENDING'::text);

--
-- Name: idx_bot_trade_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_bot_trade_status ON public.bot_trade USING btree (status);

--
-- Name: idx_bot_trade_status_opened; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_bot_trade_status_opened ON public.bot_trade USING btree (status, opened_at DESC);

--
-- Name: idx_macro_obs_series_date; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_macro_obs_series_date ON public.macro_observations USING btree (series_code, observation_date DESC);

--
-- Name: idx_macro_state_as_of_date; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_macro_state_as_of_date ON public.macro_state USING btree (as_of_date DESC);

--
-- Name: idx_push_subscriptions_user_email; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_push_subscriptions_user_email ON public.push_subscriptions USING btree (user_email);

--
-- Name: inflation_uq; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX inflation_uq ON public.inflation USING btree (year, month);

--
-- Name: inflation_year_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX inflation_year_idx ON public.inflation USING btree (year);

--
-- Name: ix_crypto_trades_exchange; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_crypto_trades_exchange ON public.crypto_trades USING btree (exchange, trade_date);

--
-- Name: ix_crypto_trades_ticker_date; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_crypto_trades_ticker_date ON public.crypto_trades USING btree (ticker, trade_date);

--
-- Name: ix_crypto_trades_trade_date; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_crypto_trades_trade_date ON public.crypto_trades USING btree (trade_date);

--
-- Name: ix_historia_portfolio_date; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_historia_portfolio_date ON public.historia USING btree (portfolio, snapshot_date);

--
-- Name: positions_exchange_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX positions_exchange_idx ON public.positions USING btree (exchange);

--
-- Name: positions_one_primary_uq; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX positions_one_primary_uq ON public.positions USING btree (asset_id) WHERE is_primary;

--
-- Name: positions_portfolio_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX positions_portfolio_idx ON public.positions USING btree (portfolio);

--
-- Name: positions_uq; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX positions_uq ON public.positions USING btree (ticker, exchange, portfolio);

--
-- Name: price_cache_fetched_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX price_cache_fetched_at_idx ON public.price_cache USING btree (fetched_at DESC);

--
-- Name: price_cache_ticker_uq; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX price_cache_ticker_uq ON public.price_cache USING btree (ticker);

--
-- Name: ps_planned_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ps_planned_at_idx ON public.purchase_sessions USING btree (planned_at DESC);

--
-- Name: ps_single_planned_uq; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX ps_single_planned_uq ON public.purchase_sessions USING btree ((1)) WHERE (status = 'PLANNED'::public.session_status_enum);

--
-- Name: ps_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ps_status_idx ON public.purchase_sessions USING btree (status);

--
-- Name: psi_session_migration_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX psi_session_migration_idx ON public.purchase_session_items USING btree (session_id) WHERE (is_migration_sell OR is_migration_buy);

--
-- Name: psi_session_portfolio_ticker_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX psi_session_portfolio_ticker_idx ON public.purchase_session_items USING btree (session_id, portfolio, ticker);

--
-- Name: tax_returns_uq; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX tax_returns_uq ON public.tax_returns USING btree (year);

--
-- Name: account_cap_adjustment account_cap_adjustment_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER account_cap_adjustment_updated_at BEFORE UPDATE ON public.account_cap_adjustment FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

--
-- Name: asset_news asset_news_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER asset_news_updated_at BEFORE UPDATE ON public.asset_news FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

--
-- Name: assets assets_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER assets_updated_at BEFORE UPDATE ON public.assets FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

--
-- Name: bot_calendar_event bot_calendar_event_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER bot_calendar_event_updated_at BEFORE UPDATE ON public.bot_calendar_event FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

--
-- Name: bot_control bot_control_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER bot_control_updated_at BEFORE UPDATE ON public.bot_control FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

--
-- Name: bot_daily_plan bot_daily_plan_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER bot_daily_plan_updated_at BEFORE UPDATE ON public.bot_daily_plan FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

--
-- Name: bot_trade bot_trade_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER bot_trade_updated_at BEFORE UPDATE ON public.bot_trade FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

--
-- Name: macro_observations macro_observations_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER macro_observations_updated_at BEFORE UPDATE ON public.macro_observations FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

--
-- Name: macro_state macro_state_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER macro_state_updated_at BEFORE UPDATE ON public.macro_state FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

--
-- Name: positions positions_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER positions_updated_at BEFORE UPDATE ON public.positions FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

--
-- Name: purchase_session_items purchase_session_items_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER purchase_session_items_updated_at BEFORE UPDATE ON public.purchase_session_items FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

--
-- Name: purchase_sessions purchase_sessions_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER purchase_sessions_updated_at BEFORE UPDATE ON public.purchase_sessions FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

--
-- Name: push_subscriptions push_subscriptions_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER push_subscriptions_updated_at BEFORE UPDATE ON public.push_subscriptions FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

--
-- Name: settings settings_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER settings_updated_at BEFORE UPDATE ON public.settings FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

--
-- Name: tax_returns tax_returns_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER tax_returns_updated_at BEFORE UPDATE ON public.tax_returns FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

--
-- Name: asset_purchases asset_purchases_session_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.asset_purchases
    ADD CONSTRAINT asset_purchases_session_id_fkey FOREIGN KEY (session_id) REFERENCES public.purchase_sessions(id);

--
-- Name: binance_transfers binance_transfers_session_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binance_transfers
    ADD CONSTRAINT binance_transfers_session_id_fkey FOREIGN KEY (session_id) REFERENCES public.purchase_sessions(id) ON DELETE SET NULL;

--
-- Name: bot_plan_scenario bot_plan_scenario_calendar_event_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_plan_scenario
    ADD CONSTRAINT bot_plan_scenario_calendar_event_id_fkey FOREIGN KEY (calendar_event_id) REFERENCES public.bot_calendar_event(id) ON DELETE SET NULL;

--
-- Name: bot_plan_scenario bot_plan_scenario_plan_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_plan_scenario
    ADD CONSTRAINT bot_plan_scenario_plan_id_fkey FOREIGN KEY (plan_id) REFERENCES public.bot_daily_plan(id) ON DELETE CASCADE;

--
-- Name: bot_signal bot_signal_calendar_event_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_signal
    ADD CONSTRAINT bot_signal_calendar_event_id_fkey FOREIGN KEY (calendar_event_id) REFERENCES public.bot_calendar_event(id) ON DELETE SET NULL;

--
-- Name: bot_signal bot_signal_plan_scenario_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_signal
    ADD CONSTRAINT bot_signal_plan_scenario_id_fkey FOREIGN KEY (plan_scenario_id) REFERENCES public.bot_plan_scenario(id) ON DELETE SET NULL;

--
-- Name: bot_trade_action bot_trade_action_trade_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_trade_action
    ADD CONSTRAINT bot_trade_action_trade_id_fkey FOREIGN KEY (trade_id) REFERENCES public.bot_trade(id) ON DELETE CASCADE;

--
-- Name: bot_trade bot_trade_calendar_event_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_trade
    ADD CONSTRAINT bot_trade_calendar_event_id_fkey FOREIGN KEY (calendar_event_id) REFERENCES public.bot_calendar_event(id) ON DELETE SET NULL;

--
-- Name: bot_trade bot_trade_signal_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_trade
    ADD CONSTRAINT bot_trade_signal_id_fkey FOREIGN KEY (signal_id) REFERENCES public.bot_signal(id) ON DELETE SET NULL;

--
-- Name: bot_trade_tick bot_trade_tick_trade_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_trade_tick
    ADD CONSTRAINT bot_trade_tick_trade_id_fkey FOREIGN KEY (trade_id) REFERENCES public.bot_trade(id) ON DELETE CASCADE;

--
-- Name: positions positions_asset_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.positions
    ADD CONSTRAINT positions_asset_fk FOREIGN KEY (asset_id) REFERENCES public.assets(id) ON DELETE CASCADE;

--
-- Name: purchase_session_items purchase_session_items_session_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.purchase_session_items
    ADD CONSTRAINT purchase_session_items_session_id_fkey FOREIGN KEY (session_id) REFERENCES public.purchase_sessions(id) ON DELETE CASCADE;

--
-- PostgreSQL database dump complete
--

