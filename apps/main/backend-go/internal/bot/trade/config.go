package trade

import (
	"os"
	"strconv"
	"strings"

	"github.com/shopspring/decimal"
)

// ExecutionConfig is the Go port of BotExecutionProperties (app.bot.execution.*):
// sizing constants, guard windows, and the two safety flags. Every value defaults
// exactly to application.yml; each is tunable via the BOT_* env var without a
// redeploy — same contract as Java's @ConfigurationProperties.
type ExecutionConfig struct {
	BaseRiskPct               decimal.Decimal
	MinRiskPct                decimal.Decimal
	MaxRiskPct                decimal.Decimal
	StopDistancePct           decimal.Decimal
	LeverageLossCapPct        decimal.Decimal
	MaxConcurrentTrades       int
	DuplicateGuardSeconds     int64
	ClusterWaitSeconds        int64
	WatchBeforeSeconds        int64
	WatchAfterSeconds         int64
	ActualsRefreshSeconds     int64
	StaleAfterSeconds         int64
	RefreshStartOffsetSeconds int64
	RefreshStopBufferSeconds  int64
	CalendarRetentionDays     int
	// LiveExecutionEnabled gates LiveExecutionService.Execute as its FIRST
	// instruction. Default OFF (empty/anything-but-"true" → blocked): even after
	// this domain's Nginx block flips, no live order is placed until the flag is
	// explicitly enabled after shadow/paper burn-in (Phase 5 acceptance).
	LiveExecutionEnabled bool
	// OkxDemoMode mirrors OKX_DEMO_MODE (default true — a missing env var can never
	// default the backend to live trading). Matrix: OFF→never any order;
	// ON+demo→demo orders; ON+live→live orders (explicit go-ahead only).
	OkxDemoMode bool
}

// DefaultExecutionConfig returns the application.yml defaults verbatim.
func DefaultExecutionConfig() ExecutionConfig {
	return ExecutionConfig{
		BaseRiskPct:               decimal.RequireFromString("0.01"),
		MinRiskPct:                decimal.RequireFromString("0.003"),
		MaxRiskPct:                decimal.RequireFromString("0.03"),
		StopDistancePct:           decimal.RequireFromString("0.01"),
		LeverageLossCapPct:        decimal.RequireFromString("0.30"),
		MaxConcurrentTrades:       1,
		DuplicateGuardSeconds:     60,
		ClusterWaitSeconds:        20,
		WatchBeforeSeconds:        30,
		WatchAfterSeconds:         300,
		ActualsRefreshSeconds:     5,
		StaleAfterSeconds:         45,
		RefreshStartOffsetSeconds: 15, // 2026-09-29: was 5; early polls burned ForexFactory's rate budget (429 backoff outlasted the window)
		RefreshStopBufferSeconds:  15,
		CalendarRetentionDays:     30,
		LiveExecutionEnabled:      false,
		OkxDemoMode:               true,
	}
}

// LoadExecutionConfig overlays BOT_*/OKX_* env vars on the defaults.
func LoadExecutionConfig() ExecutionConfig {
	cfg := DefaultExecutionConfig()
	if v := decimalEnv("BOT_BASE_RISK_PCT"); v != nil {
		cfg.BaseRiskPct = *v
	}
	if v := decimalEnv("BOT_MIN_RISK_PCT"); v != nil {
		cfg.MinRiskPct = *v
	}
	if v := decimalEnv("BOT_MAX_RISK_PCT"); v != nil {
		cfg.MaxRiskPct = *v
	}
	if v := decimalEnv("BOT_STOP_DISTANCE_PCT"); v != nil {
		cfg.StopDistancePct = *v
	}
	if v := decimalEnv("BOT_LEVERAGE_LOSS_CAP_PCT"); v != nil {
		cfg.LeverageLossCapPct = *v
	}
	if v := intEnv("BOT_DUPLICATE_GUARD_SECONDS"); v != nil {
		cfg.DuplicateGuardSeconds = *v
	}
	if v := intEnv("BOT_CLUSTER_WAIT_SECONDS"); v != nil {
		cfg.ClusterWaitSeconds = *v
	}
	if v := intEnv("BOT_STALE_AFTER_SECONDS"); v != nil {
		cfg.StaleAfterSeconds = *v
	}
	if v := intEnv("BOT_CALENDAR_RETENTION_DAYS"); v != nil {
		cfg.CalendarRetentionDays = int(*v)
	}
	cfg.LiveExecutionEnabled = boolEnv("BOT_LIVE_EXECUTION_ENABLED")
	// OKX_DEMO_MODE defaults true: only an explicit "false" arms live trading.
	if raw, ok := os.LookupEnv("OKX_DEMO_MODE"); ok {
		cfg.OkxDemoMode = strings.ToLower(strings.TrimSpace(raw)) != "false"
	}
	return cfg
}

// LiveExecutionFlag is the standalone gate read (documented for DevOps wiring):
// BOT_LIVE_EXECUTION_ENABLED=true (case-insensitive, trimmed) arms live orders.
// Empty or anything else → blocked (SKIPPED_FLAG_DISABLED, zero OKX calls).
func LiveExecutionFlag() bool {
	return boolEnv("BOT_LIVE_EXECUTION_ENABLED")
}

func boolEnv(key string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	return v == "true" || v == "1"
}

func decimalEnv(key string) *decimal.Decimal {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}
	d, err := decimal.NewFromString(raw)
	if err != nil {
		return nil
	}
	return &d
}

func intEnv(key string) *int64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil
	}
	return &v
}
