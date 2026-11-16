package stats

import (
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/oapi-codegen/runtime/types"

	"investing-app/backend-go/internal/bot/botutil"
	"investing-app/backend-go/openapi"
	"investing-app/common-go/httpx"
)

// Handler adapts Service to the oapi-codegen ServerInterface methods for
// BotStatsController's endpoints — the same thin-Facade convention as prior
// phases (no business logic here; service -> openapi type mapping only).
type Handler struct {
	service *Service
}

// NewHandler constructs a Handler around service.
func NewHandler(service *Service) *Handler { return &Handler{service: service} }

// GetBotStats implements GET /bot/stats (dual-audience read) — the direct port of
// BotStatsController.stats.
func (h *Handler) GetBotStats(w http.ResponseWriter, r *http.Request) {
	dto, err := h.service.Stats(r.Context())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, openapi.BotStatsResponse{
		Data: openapi.BotStats{
			AvgHoldMinutes: dto.AvgHoldMinutes,
			BestTrade:      dto.BestTrade,
			ClosedTrades:   dto.ClosedTrades,
			OpenTrades:     dto.OpenTrades,
			TotalPnlPct:    dto.TotalPnlPct, // always nil — reserved, like Java
			TotalPnlPln:    dto.TotalPnlPln.InexactFloat64(),
			TotalTrades:    dto.TotalTrades,
			WinRate:        dto.WinRate,
		},
		Meta: botutil.NewMeta(),
	})
}

// GetBotEquity implements GET /bot/equity (human-only) — the direct port of
// BotStatsController.equity. Daily cumulative PnL, date ASC.
func (h *Handler) GetBotEquity(w http.ResponseWriter, r *http.Request) {
	points, err := h.service.Equity(r.Context())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	data := make([]openapi.BotEquityPoint, 0, len(points))
	for _, p := range points {
		data = append(data, openapi.BotEquityPoint{
			CumulativePnlPln: p.CumulativePnlPln.InexactFloat64(),
			DailyPnlPln:      p.DailyPnlPln.InexactFloat64(),
			Date:             types.Date{Time: p.Date},
		})
	}
	httpx.WriteJSON(w, http.StatusOK, openapi.BotEquityListResponse{
		Data: data, Meta: botutil.NewMeta(),
	})
}

// ListBotOpenPositions implements GET /bot/positions/open (human-only, non-paged
// envelope) — the direct port of BotStatsController.openPositions. Same rows as GET
// /bot/trades?status=OPEN but a ListResponse envelope with no page member — the
// byte-for-byte quirk the frontend normalizes; page/size params are accepted and
// applied, never echoed.
func (h *Handler) ListBotOpenPositions(w http.ResponseWriter, r *http.Request, params openapi.ListBotOpenPositionsParams) {
	page, size, appErr := botutil.PageSize(params.Page, params.Size, 0, 20)
	if appErr != nil {
		httpx.WriteError(w, appErr)
		return
	}
	trades, err := h.service.OpenPositions(r.Context(), page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, openapi.BotTradeListResponse{
		Data: trades, Meta: botutil.NewMeta(),
	})
}

func isNoRows(err error) bool {
	return err != nil && (err == pgx.ErrNoRows || strings.Contains(err.Error(), "no rows"))
}
