package trade

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/internal/bot/botutil"
	"investing-app/backend-go/openapi"
	"investing-app/common-go/httpx"
	"investing-app/common-go/pgnum"
)

// Handler adapts Service to the oapi-codegen ServerInterface methods for
// BotTradeController's endpoints — the same thin-Facade convention as prior
// phases (no business logic here; service -> openapi type mapping only, this
// migration's hand-written MapStruct equivalent).
type Handler struct {
	service *Service
}

// NewHandler constructs a Handler around service.
func NewHandler(service *Service) *Handler { return &Handler{service: service} }

// ListBotTrades implements GET /bot/trades (dual-audience read) — the direct port
// of BotTradeController.list. openedAt DESC, paged, live-price enriched.
func (h *Handler) ListBotTrades(w http.ResponseWriter, r *http.Request, params openapi.ListBotTradesParams) {
	page, size, appErr := botutil.PageSize(params.Page, params.Size, 0, 20)
	if appErr != nil {
		httpx.WriteError(w, appErr)
		return
	}
	var status, side string
	if params.Status != nil {
		status = string(*params.Status)
	}
	if params.Side != nil {
		side = string(*params.Side)
	}
	result, err := h.service.List(r.Context(), status, side, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, openapi.BotTradePagedResponse{
		Data: result.Data, Meta: botutil.NewMeta(),
		Page: openapi.PageInfo{Number: result.Page, Size: result.Size, Total: int(result.Total)},
	})
}

// CreateBotTrade implements POST /bot/trades (machine-only, kill-switch + guards)
// — the direct port of BotTradeController.create. Answers 201; paused/duplicate/
// concurrent entries answer 409.
func (h *Handler) CreateBotTrade(w http.ResponseWriter, r *http.Request) {
	in, appErr := decodeCreateBody(r)
	if appErr != nil {
		httpx.WriteError(w, appErr)
		return
	}
	trade, err := h.service.Create(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, openapi.BotTradeSingleResponse{
		Data: trade, Meta: botutil.NewMeta(),
	})
}

// GetBotTrade implements GET /bot/trades/{id} (dual-audience read) — the direct
// port of BotTradeController.get. Unknown id answers 404.
func (h *Handler) GetBotTrade(w http.ResponseWriter, r *http.Request, id int64) {
	trade, err := h.service.Get(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, openapi.BotTradeSingleResponse{
		Data: trade, Meta: botutil.NewMeta(),
	})
}

// ListPendingScalpTrades implements GET /bot/trades/pending-scalp (machine-only) —
// the direct port of BotTradeController.pendingScalp. PENDING rows, oldest first,
// in the non-paged ListResponse envelope (no page member — the byte-for-byte quirk
// the launcher normalizes). Intentionally ignores trading_enabled.
func (h *Handler) ListPendingScalpTrades(w http.ResponseWriter, r *http.Request) {
	trades, err := h.service.PendingScalp(r.Context())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, openapi.BotTradeListResponse{
		Data: trades, Meta: botutil.NewMeta(),
	})
}

// ClaimScalpSession implements POST /bot/trades/{id}/scalp-claim (machine-only) —
// the direct port of BotTradeController.claimScalp. 200 = claimed; 409 = someone
// else won (not an error to the launcher).
func (h *Handler) ClaimScalpSession(w http.ResponseWriter, r *http.Request, id int64) {
	trade, err := h.service.Claim(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, openapi.BotTradeSingleResponse{
		Data: trade, Meta: botutil.NewMeta(),
	})
}

// FinishScalpSession implements POST /bot/trades/{id}/scalp-finish (machine-only)
// — the direct port of BotTradeController.finishScalp. Only RUNNING rows
// transition; anything else → 409. Missing exitCode records "unknown".
func (h *Handler) FinishScalpSession(w http.ResponseWriter, r *http.Request, id int64) {
	var exitCode *int
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err == nil {
		var body openapi.ScalpFinishRequest
		reassembled, _ := json.Marshal(raw)
		if err := json.Unmarshal(reassembled, &body); err == nil {
			exitCode = body.ExitCode
		}
	}
	trade, err := h.service.Finish(r.Context(), id, exitCode)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, openapi.BotTradeSingleResponse{
		Data: trade, Meta: botutil.NewMeta(),
	})
}

// CloseBotTrade implements PATCH /bot/trades/{id}/close (machine-only, never
// kill-switch-gated) — the direct port of BotTradeController.close.
func (h *Handler) CloseBotTrade(w http.ResponseWriter, r *http.Request, id int64) {
	in, appErr := decodeCloseBody(r)
	if appErr != nil {
		httpx.WriteError(w, appErr)
		return
	}
	trade, err := h.service.Close(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, openapi.BotTradeSingleResponse{
		Data: trade, Meta: botutil.NewMeta(),
	})
}

// AddBotTradeAction implements POST /bot/trades/{id}/actions (machine-only) — the
// direct port of BotTradeController.addAction. Answers 201 with the stored action
// (the durable audit row).
func (h *Handler) AddBotTradeAction(w http.ResponseWriter, r *http.Request, id int64) {
	in, appErr := decodeActionBody(r)
	if appErr != nil {
		httpx.WriteError(w, appErr)
		return
	}
	row, err := h.service.AddAction(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, openapi.BotTradeActionSingleResponse{
		Data: toOpenAPIAction(row), Meta: botutil.NewMeta(),
	})
}

// ListBotTradeActions implements GET /bot/trades/{id}/actions (dual-audience read)
// — the direct port of BotTradeController.listActions. Oldest first.
func (h *Handler) ListBotTradeActions(w http.ResponseWriter, r *http.Request, id int64) {
	rows, err := h.service.ListActions(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	data := make([]openapi.BotTradeAction, 0, len(rows))
	for _, a := range rows {
		data = append(data, toOpenAPIAction(a))
	}
	httpx.WriteJSON(w, http.StatusOK, openapi.BotTradeActionListResponse{
		Data: data, Meta: botutil.NewMeta(),
	})
}

// AddBotTradeTicks implements POST /bot/trades/{id}/ticks (machine-only) — the
// direct port of BotTradeController.addTicks. Answers 201 with the STORED TICKS
// LIST (not a single resource — the wire quirk the spec pins); empty batch →
// empty list, still 201.
func (h *Handler) AddBotTradeTicks(w http.ResponseWriter, r *http.Request, id int64) {
	ticks, appErr := decodeTicksBody(r)
	if appErr != nil {
		httpx.WriteError(w, appErr)
		return
	}
	rows, err := h.service.AddTicks(r.Context(), id, ticks)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	data := make([]openapi.BotTradeTick, 0, len(rows))
	for _, t := range rows {
		one, err := toOpenAPITick(t)
		if err != nil {
			httpx.WriteError(w, err)
			return
		}
		data = append(data, one)
	}
	httpx.WriteJSON(w, http.StatusCreated, openapi.BotTradeTickListResponse{
		Data: data, Meta: botutil.NewMeta(),
	})
}

// ListBotTradeTicks implements GET /bot/trades/{id}/ticks (dual-audience read) —
// the direct port of BotTradeController.listTicks. Oldest first; the range applies
// only when BOTH from and to are present.
func (h *Handler) ListBotTradeTicks(w http.ResponseWriter, r *http.Request, id int64, params openapi.ListBotTradeTicksParams) {
	rows, err := h.service.ListTicks(r.Context(), id, params.From, params.To)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	data := make([]openapi.BotTradeTick, 0, len(rows))
	for _, t := range rows {
		one, err := toOpenAPITick(t)
		if err != nil {
			httpx.WriteError(w, err)
			return
		}
		data = append(data, one)
	}
	httpx.WriteJSON(w, http.StatusOK, openapi.BotTradeTickListResponse{
		Data: data, Meta: botutil.NewMeta(),
	})
}

// RequestBotTradeManage implements POST /bot/trades/{id}/request-manage
// (machine-only, no-op stub) — the direct port of BotTradeController.requestManage.
// Stateless 202 acknowledge: no DB write, no queue, no trade lookup (even an
// unknown id answers 202 — exactly like Java, which never touches the service).
// The contract is kept so a future queued executor slots in without a wire change.
func (h *Handler) RequestBotTradeManage(w http.ResponseWriter, r *http.Request, id int64) {
	httpx.WriteJSON(w, http.StatusAccepted, openapi.BotManageAckResponse{
		Data: openapi.BotManageAck{Status: "accepted", TradeId: id},
		Meta: botutil.NewMeta(),
	})
}

// ── Body decoders ────────────────────────────────────────────────────────────

func decodeCreateBody(r *http.Request) (CreateInput, *httpx.AppError) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		return CreateInput{}, botutil.Unprocessable("", "Invalid request body")
	}
	for _, field := range []string{"side", "quantity", "entryPrice"} {
		if v, ok := raw[field]; !ok || string(v) == "null" {
			return CreateInput{}, botutil.Unprocessable(field, "must not be null")
		}
	}
	var body openapi.CreateBotTradeRequest
	reassembled, _ := json.Marshal(raw)
	if err := json.Unmarshal(reassembled, &body); err != nil {
		return CreateInput{}, botutil.Unprocessable("", "Invalid request body")
	}
	in := CreateInput{
		SignalID: body.SignalId, CalendarEventID: body.CalendarEventId,
		Side:     string(body.Side),
		Quantity: decimal.NewFromFloat(body.Quantity), QuantitySet: true,
		EntryPrice: decimal.NewFromFloat(body.EntryPrice), EntryPriceSet: true,
		OkxOrderID: body.OkxOrderId, Notes: body.Notes,
	}
	if body.Leverage != nil {
		v := *body.Leverage
		in.Leverage = &v
	}
	return in, nil
}

func decodeCloseBody(r *http.Request) (CloseInput, *httpx.AppError) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		return CloseInput{}, botutil.Unprocessable("", "Invalid request body")
	}
	if v, ok := raw["exitPrice"]; !ok || string(v) == "null" {
		return CloseInput{}, botutil.Unprocessable("exitPrice", "must not be null")
	}
	var body openapi.CloseBotTradeRequest
	reassembled, _ := json.Marshal(raw)
	if err := json.Unmarshal(reassembled, &body); err != nil {
		return CloseInput{}, botutil.Unprocessable("", "Invalid request body")
	}
	in := CloseInput{ExitPrice: decimal.NewFromFloat(body.ExitPrice), ExitPriceSet: true}
	if body.PnlPln != nil {
		v := decimal.NewFromFloat(*body.PnlPln)
		in.PnlPln = &v
	}
	if body.PnlPct != nil {
		v := decimal.NewFromFloat(*body.PnlPct)
		in.PnlPct = &v
	}
	return in, nil
}

func decodeActionBody(r *http.Request) (ActionInput, *httpx.AppError) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		return ActionInput{}, botutil.Unprocessable("", "Invalid request body")
	}
	if v, ok := raw["actionType"]; !ok || string(v) == "null" {
		return ActionInput{}, botutil.Unprocessable("actionType", "must not be null")
	}
	var body openapi.CreateBotTradeActionRequest
	reassembled, _ := json.Marshal(raw)
	if err := json.Unmarshal(reassembled, &body); err != nil {
		return ActionInput{}, botutil.Unprocessable("", "Invalid request body")
	}
	in := ActionInput{ActionType: string(body.ActionType), Reasoning: body.Reasoning}
	if body.Price != nil {
		v := decimal.NewFromFloat(*body.Price)
		in.Price = &v
	}
	if body.Quantity != nil {
		v := decimal.NewFromFloat(*body.Quantity)
		in.Quantity = &v
	}
	if body.Source != nil {
		v := string(*body.Source)
		in.Source = &v
	}
	return in, nil
}

func decodeTicksBody(r *http.Request) ([]TickInput, *httpx.AppError) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		return nil, botutil.Unprocessable("", "Invalid request body")
	}
	v, ok := raw["ticks"]
	if !ok || string(v) == "null" {
		return nil, botutil.Unprocessable("ticks", "must not be null")
	}
	var body openapi.CreateBotTradeTicksRequest
	reassembled, _ := json.Marshal(raw)
	if err := json.Unmarshal(reassembled, &body); err != nil {
		return nil, botutil.Unprocessable("", "Invalid request body")
	}
	out := make([]TickInput, 0, len(body.Ticks))
	for _, t := range body.Ticks {
		out = append(out, TickInput{
			Price:          floatPtr(t.Price),
			Volume:         floatPtr(t.Volume),
			Cvd:            floatPtr(t.Cvd),
			Atr1m:          floatPtr(t.Atr1m),
			ObImbalance:    floatPtr(t.ObImbalance),
			FundingRate:    floatPtr(t.FundingRate),
			LongShortRatio: floatPtr(t.LongShortRatio),
			OpenInterest:   floatPtr(t.OpenInterest),
		})
	}
	return out, nil
}

func floatPtr(f *float64) *decimal.Decimal {
	if f == nil {
		return nil
	}
	v := decimal.NewFromFloat(*f)
	return &v
}

// ── Row -> openapi mapping ───────────────────────────────────────────────────

func toOpenAPIAction(a sqlcgen.BotTradeAction) openapi.BotTradeAction {
	out := openapi.BotTradeAction{
		ActionType: openapi.BotTradeActionActionType(a.ActionType),
		CreatedAt:  a.CreatedAt.Time.UTC(),
		Id:         a.ID,
		Reasoning:  a.Reasoning,
		Source:     openapi.BotTradeActionSource(a.Source),
		TradeId:    a.TradeID,
	}
	if a.Price.Valid {
		if v, err := pgnum.ToDecimal(a.Price); err == nil {
			f := v.InexactFloat64()
			out.Price = &f
		}
	}
	if a.Quantity.Valid {
		if v, err := pgnum.ToDecimal(a.Quantity); err == nil {
			f := v.InexactFloat64()
			out.Quantity = &f
		}
	}
	return out
}

func toOpenAPITick(t sqlcgen.BotTradeTick) (openapi.BotTradeTick, error) {
	price, err := pgnum.ToDecimal(t.Price)
	if err != nil {
		return openapi.BotTradeTick{}, err
	}
	out := openapi.BotTradeTick{
		CreatedAt: t.CreatedAt.Time.UTC(),
		Id:        t.ID, Price: price.InexactFloat64(), TradeId: t.TradeID,
	}
	setTickFloat := func(n pgtype.Numeric, dest **float64) error {
		if n.Valid {
			v, err := pgnum.ToDecimal(n)
			if err != nil {
				return err
			}
			f := v.InexactFloat64()
			*dest = &f
		}
		return nil
	}
	if err := setTickFloat(t.Volume, &out.Volume); err != nil {
		return openapi.BotTradeTick{}, err
	}
	if err := setTickFloat(t.Cvd, &out.Cvd); err != nil {
		return openapi.BotTradeTick{}, err
	}
	// Serialized as atr1m (Java record component name), never atr_1m — pinned by
	// the generated json tag, asserted in handler tests.
	if err := setTickFloat(t.Atr1m, &out.Atr1m); err != nil {
		return openapi.BotTradeTick{}, err
	}
	if err := setTickFloat(t.ObImbalance, &out.ObImbalance); err != nil {
		return openapi.BotTradeTick{}, err
	}
	if err := setTickFloat(t.FundingRate, &out.FundingRate); err != nil {
		return openapi.BotTradeTick{}, err
	}
	if err := setTickFloat(t.LongShortRatio, &out.LongShortRatio); err != nil {
		return openapi.BotTradeTick{}, err
	}
	if err := setTickFloat(t.OpenInterest, &out.OpenInterest); err != nil {
		return openapi.BotTradeTick{}, err
	}
	return out, nil
}
