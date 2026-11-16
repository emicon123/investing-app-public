-- Baseline schema of portfolio-c (squashed).
--
-- The private development history had incremental migrations, some of which loaded real
-- portfolio data. For this public copy they were squashed into one schema-only baseline, generated
-- with `pg_dump --schema-only` after applying the full history to an empty PostgreSQL 17 database.
-- No rows are included here; see V2 for default reference data.

--
-- PostgreSQL database dump
--

-- Dumped from database version 17.11
-- Dumped by pg_dump version 17.11

--
-- Name: asset_class_enum; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.asset_class_enum AS ENUM (
    'ETF',
    'METALS',
    'CRYPTO',
    'STOCK',
    'CURRENCY'
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
    'BINANCE',
    'OTHER'
);

--
-- Name: exposure_enum; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.exposure_enum AS ENUM (
    'CRYPTO',
    'METALS',
    'STOCKS',
    'COMMODITIES',
    'CURRENCY'
);

--
-- Name: portfolio_enum; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.portfolio_enum AS ENUM (
    'VARIABLE',
    'FIXED'
);

--
-- Name: set_updated_at(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.set_updated_at() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN NEW.updated_at = NOW(); RETURN NEW; END;
$$;

--
-- Name: account_cash_snapshot; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.account_cash_snapshot (
    portfolio public.portfolio_enum NOT NULL,
    balance_pln numeric(18,4) DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    update_reason character varying(20) DEFAULT 'MANUAL'::character varying NOT NULL,
    CONSTRAINT account_cash_snapshot_balance_pln_check CHECK ((balance_pln >= (0)::numeric))
);

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
-- Name: deposits; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.deposits (
    id bigint NOT NULL,
    portfolio public.portfolio_enum NOT NULL,
    deposit_date date NOT NULL,
    amount numeric(18,4) NOT NULL,
    comment text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT deposits_amount_check CHECK ((amount > (0)::numeric))
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
    CONSTRAINT fx_rate_cache_rate_to_pln_check CHECK ((rate_to_pln > (0)::numeric))
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
    portfolio public.portfolio_enum NOT NULL,
    snapshot_date date NOT NULL,
    total_deposits numeric(15,2) NOT NULL,
    portfolio_value numeric(15,2) NOT NULL,
    xirr numeric(8,6),
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    position_values jsonb
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
-- Name: position_transfers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.position_transfers (
    id bigint NOT NULL,
    transfer_id uuid NOT NULL,
    portfolio public.portfolio_enum NOT NULL,
    ticker character varying(20) NOT NULL,
    units_delta numeric(18,8) NOT NULL,
    price_pln numeric(18,8) NOT NULL,
    total_pln numeric(18,4) NOT NULL,
    transferred_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT position_transfers_price_pln_check CHECK ((price_pln > (0)::numeric)),
    CONSTRAINT position_transfers_units_delta_nonzero_chk CHECK ((units_delta <> (0)::numeric))
);

--
-- Name: position_transfers_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.position_transfers_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: position_transfers_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.position_transfers_id_seq OWNED BY public.position_transfers.id;

--
-- Name: positions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.positions (
    id bigint NOT NULL,
    portfolio public.portfolio_enum NOT NULL,
    ticker character varying(20) NOT NULL,
    name character varying(200) NOT NULL,
    exchange public.exchange_enum NOT NULL,
    asset_class public.asset_class_enum NOT NULL,
    exposure public.exposure_enum NOT NULL,
    price_currency public.currency_enum NOT NULL,
    units numeric(18,8) DEFAULT 0 NOT NULL,
    total_spent_pln numeric(18,4),
    avg_buy_price numeric(18,8),
    group_name character varying(100),
    yahoo_ticker character varying(50),
    sort_order integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    asset_id bigint NOT NULL,
    is_primary boolean DEFAULT false NOT NULL,
    deleted_at timestamp with time zone,
    CONSTRAINT positions_units_chk CHECK ((units >= (0)::numeric))
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
-- Name: purchase_lots; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.purchase_lots (
    id bigint NOT NULL,
    portfolio public.portfolio_enum NOT NULL,
    session_id uuid NOT NULL,
    purchased_at date NOT NULL,
    ticker character varying(20) NOT NULL,
    units numeric(18,8) NOT NULL,
    price_pln numeric(18,8) NOT NULL,
    cost_pln numeric(18,4) NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT purchase_lots_cost_pln_check CHECK ((cost_pln > (0)::numeric)),
    CONSTRAINT purchase_lots_price_pln_check CHECK ((price_pln > (0)::numeric)),
    CONSTRAINT purchase_lots_units_check CHECK ((units > (0)::numeric))
);

--
-- Name: purchase_lots_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.purchase_lots_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: purchase_lots_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.purchase_lots_id_seq OWNED BY public.purchase_lots.id;

--
-- Name: settings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.settings (
    key character varying(100) NOT NULL,
    value text NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: assets id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.assets ALTER COLUMN id SET DEFAULT nextval('public.assets_id_seq'::regclass);

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
-- Name: position_transfers id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.position_transfers ALTER COLUMN id SET DEFAULT nextval('public.position_transfers_id_seq'::regclass);

--
-- Name: positions id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.positions ALTER COLUMN id SET DEFAULT nextval('public.positions_id_seq'::regclass);

--
-- Name: price_cache id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.price_cache ALTER COLUMN id SET DEFAULT nextval('public.price_cache_id_seq'::regclass);

--
-- Name: purchase_lots id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.purchase_lots ALTER COLUMN id SET DEFAULT nextval('public.purchase_lots_id_seq'::regclass);

--
-- Name: account_cash_snapshot account_cash_snapshot_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.account_cash_snapshot
    ADD CONSTRAINT account_cash_snapshot_pkey PRIMARY KEY (portfolio);

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
-- Name: deposits deposits_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.deposits
    ADD CONSTRAINT deposits_pkey PRIMARY KEY (id);

--
-- Name: fx_rate_cache fx_rate_cache_from_currency_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.fx_rate_cache
    ADD CONSTRAINT fx_rate_cache_from_currency_key UNIQUE (from_currency);

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
-- Name: historia historia_portfolio_date_uq; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.historia
    ADD CONSTRAINT historia_portfolio_date_uq UNIQUE (portfolio, snapshot_date);

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
-- Name: position_transfers position_transfers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.position_transfers
    ADD CONSTRAINT position_transfers_pkey PRIMARY KEY (id);

--
-- Name: positions positions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.positions
    ADD CONSTRAINT positions_pkey PRIMARY KEY (id);

--
-- Name: positions positions_ticker_portfolio_uq; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.positions
    ADD CONSTRAINT positions_ticker_portfolio_uq UNIQUE (ticker, portfolio);

--
-- Name: price_cache price_cache_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.price_cache
    ADD CONSTRAINT price_cache_pkey PRIMARY KEY (id);

--
-- Name: price_cache price_cache_ticker_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.price_cache
    ADD CONSTRAINT price_cache_ticker_key UNIQUE (ticker);

--
-- Name: purchase_lots purchase_lots_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.purchase_lots
    ADD CONSTRAINT purchase_lots_pkey PRIMARY KEY (id);

--
-- Name: purchase_lots purchase_lots_session_ticker_uq; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.purchase_lots
    ADD CONSTRAINT purchase_lots_session_ticker_uq UNIQUE (session_id, ticker);

--
-- Name: settings settings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.settings
    ADD CONSTRAINT settings_pkey PRIMARY KEY (key);

--
-- Name: deposits_portfolio_date_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX deposits_portfolio_date_idx ON public.deposits USING btree (portfolio, deposit_date);

--
-- Name: idempotency_keys_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idempotency_keys_created_at_idx ON public.idempotency_keys USING btree (created_at);

--
-- Name: idx_positions_portfolio; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_positions_portfolio ON public.positions USING btree (portfolio);

--
-- Name: inflation_uq; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX inflation_uq ON public.inflation USING btree (year, month);

--
-- Name: inflation_year_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX inflation_year_idx ON public.inflation USING btree (year);

--
-- Name: ix_historia_portfolio_date; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_historia_portfolio_date ON public.historia USING btree (portfolio, snapshot_date);

--
-- Name: position_transfers_ticker_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX position_transfers_ticker_idx ON public.position_transfers USING btree (portfolio, ticker, transferred_at DESC);

--
-- Name: position_transfers_transfer_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX position_transfers_transfer_idx ON public.position_transfers USING btree (transfer_id);

--
-- Name: positions_one_primary_uq; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX positions_one_primary_uq ON public.positions USING btree (asset_id) WHERE is_primary;

--
-- Name: purchase_lots_portfolio_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX purchase_lots_portfolio_idx ON public.purchase_lots USING btree (portfolio, purchased_at);

--
-- Name: purchase_lots_purchased_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX purchase_lots_purchased_at_idx ON public.purchase_lots USING btree (purchased_at);

--
-- Name: purchase_lots_session_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX purchase_lots_session_idx ON public.purchase_lots USING btree (session_id);

--
-- Name: assets assets_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER assets_updated_at BEFORE UPDATE ON public.assets FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

--
-- Name: positions positions_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER positions_updated_at BEFORE UPDATE ON public.positions FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

--
-- Name: settings settings_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER settings_updated_at BEFORE UPDATE ON public.settings FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

--
-- Name: position_transfers position_transfers_ticker_portfolio_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.position_transfers
    ADD CONSTRAINT position_transfers_ticker_portfolio_fk FOREIGN KEY (ticker, portfolio) REFERENCES public.positions(ticker, portfolio);

--
-- Name: positions positions_asset_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.positions
    ADD CONSTRAINT positions_asset_fk FOREIGN KEY (asset_id) REFERENCES public.assets(id) ON DELETE CASCADE;

--
-- Name: purchase_lots purchase_lots_ticker_portfolio_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.purchase_lots
    ADD CONSTRAINT purchase_lots_ticker_portfolio_fk FOREIGN KEY (ticker, portfolio) REFERENCES public.positions(ticker, portfolio);

--
-- PostgreSQL database dump complete
--

