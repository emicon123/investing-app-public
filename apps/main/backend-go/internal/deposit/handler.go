package deposit

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/oapi-codegen/runtime/types"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/openapi"
	"investing-app/common-go/decx"
	"investing-app/common-go/httpx"
)

// Handler adapts Service to the oapi-codegen ServerInterface methods for DepositsController's
// endpoints (main.go's apiServer forwards each one).
type Handler struct {
	service *Service
}

// NewHandler constructs a Handler around service.
func NewHandler(service *Service) *Handler { return &Handler{service: service} }

// ListDeposits implements GET /deposits.
func (h *Handler) ListDeposits(w http.ResponseWriter, r *http.Request, params openapi.ListDepositsParams) {
	page := 0
	if params.Page != nil {
		page = *params.Page
	}
	size := defaultPageSize
	if params.Size != nil {
		size = *params.Size
	}
	filter := ListFilter{}
	if params.Portfolio != nil {
		v := string(*params.Portfolio)
		filter.Portfolio = &v
	}
	if params.AccountType != nil {
		v := string(*params.AccountType)
		filter.AccountType = &v
	}
	if params.Exchange != nil {
		v := string(*params.Exchange)
		filter.Exchange = &v
	}
	filter.Year = params.Year

	items, total, err := h.service.List(r.Context(), filter, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	data := make([]openapi.Deposit, 0, len(items))
	for _, d := range items {
		data = append(data, toOpenAPIDeposit(d))
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.Paged(data, httpx.PageInfo{Number: page, Size: size, Total: total}))
}

// CreateDeposit implements POST /deposits.
func (h *Handler) CreateDeposit(w http.ResponseWriter, r *http.Request) {
	in, appErr := decodeCreateBody(r)
	if appErr != nil {
		httpx.WriteError(w, appErr)
		return
	}
	dto, err := h.service.Create(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, httpx.Single(toOpenAPIDeposit(dto)))
}

// GetXirr implements GET /deposits/xirr.
func (h *Handler) GetXirr(w http.ResponseWriter, r *http.Request, params openapi.GetXirrParams) {
	result, err := h.service.ComputeXirr(r.Context(), string(params.Portfolio))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	resp := openapi.XirrResponse{Meta: newOpenAPIMeta(r.Context())}
	portfolio := params.Portfolio
	resp.Data.Portfolio = &portfolio
	if result.XirrPct != nil {
		v := decx.ToFloat(*result.XirrPct)
		resp.Data.XirrPct = &v
	}
	totalInvested := decx.ToFloat(result.TotalInvestedPln)
	resp.Data.TotalInvestedPln = &totalInvested
	currentValue := decx.ToFloat(result.CurrentValuePln)
	resp.Data.CurrentValuePln = &currentValue
	if result.SimpleReturnPct != nil {
		v := decx.ToFloat(*result.SimpleReturnPct)
		resp.Data.SimpleReturnPct = &v
	}
	if result.YearsElapsed != nil {
		v := decx.ToFloat(*result.YearsElapsed)
		resp.Data.YearsElapsed = &v
	}
	now := time.Now().UTC()
	resp.Data.ComputedAt = &now
	httpx.WriteJSON(w, http.StatusOK, resp)
}

// GetAccountCapUsage implements GET /deposits/account-cap-usage.
func (h *Handler) GetAccountCapUsage(w http.ResponseWriter, r *http.Request, params openapi.GetAccountCapUsageParams) {
	items, err := h.service.GetAccountCapUsage(r.Context(), params.Year)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	data := make([]openapi.AccountCapUsage, 0, len(items))
	for _, it := range items {
		data = append(data, toOpenAPIAccountCapUsage(it))
	}
	httpx.WriteJSON(w, http.StatusOK, openapi.AccountCapUsageResponse{Data: data, Meta: newOpenAPIMeta(r.Context())})
}

// decodeCreateBody maps POST /deposits' body to the service input; the amount is a decimal on
// the generated type (x-go-type, task 182 / F3.4), the wire still takes a JSON number.
func decodeCreateBody(r *http.Request) (CreateInput, *httpx.AppError) {
	var body openapi.CreateDepositRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return CreateInput{}, unprocessable("", "Invalid request body")
	}
	return CreateInput{
		DepositDate: body.DepositDate.Time, Amount: body.Amount,
		Currency: string(body.Currency), Exchange: string(body.Exchange), AccountType: string(body.AccountType),
		Portfolio: string(body.Portfolio), Comment: body.Comment,
	}, nil
}

// accountCapAdjustmentRequest is the request body of PUT /deposits/account-cap-usage/adjustment.
// It mirrors the generated openapi.AccountCapAdjustmentRequest but keeps remainingPln a
// decimal.Decimal, so the typed limit is parsed exactly instead of through a float64.
type accountCapAdjustmentRequest struct {
	AccountType  string          `json:"accountType"`
	Year         int             `json:"year"`
	RemainingPln decimal.Decimal `json:"remainingPln"`
}

// UpsertAccountCapAdjustment implements PUT /deposits/account-cap-usage/adjustment
// (operationId upsertAccountCapAdjustment, ADR-058).
func (h *Handler) UpsertAccountCapAdjustment(w http.ResponseWriter, r *http.Request) {
	var body accountCapAdjustmentRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteError(w, unprocessable("", "Invalid request body"))
		return
	}
	item, err := h.service.UpsertAccountCapAdjustment(r.Context(), body.AccountType, body.Year, body.RemainingPln)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.Single(toOpenAPIAccountCapUsage(item)))
}

func toOpenAPIDeposit(d DepositDTO) openapi.Deposit {
	id := d.ID
	createdAt := d.CreatedAt.UTC()
	return openapi.Deposit{
		Id: &id, DepositDate: dateOf(d.DepositDate), Amount: decx.ToFloat(d.Amount),
		Currency: openapi.Currency(d.Currency), Exchange: openapi.Exchange(d.Exchange),
		AccountType: openapi.AccountType(d.AccountType), Portfolio: openapi.Portfolio(d.Portfolio),
		Comment: d.Comment, CreatedAt: &createdAt,
	}
}

func toOpenAPIAccountCapUsage(it AccountCapUsageItem) openapi.AccountCapUsage {
	accountType := openapi.AccountType(it.AccountType)
	year := it.Year
	total := decx.ToFloat(it.TotalDepositedPln)
	cap := decx.ToFloat(it.AnnualCapPln)
	remaining := decx.ToFloat(it.RemainingPln)
	util := decx.ToFloat(it.UtilizationPct)
	return openapi.AccountCapUsage{
		AccountType: &accountType, Year: &year, TotalDepositedPln: &total,
		AnnualCapPln: &cap, RemainingPln: &remaining, UtilizationPct: &util,
	}
}

func unprocessable(field, message string) *httpx.AppError {
	return &httpx.AppError{Status: 422, Code: httpx.CodeValidationError, Field: field, Message: message}
}

// newOpenAPIMeta builds the generated openapi.Meta for the response shapes that carry their own
// meta field instead of an httpx envelope; its requestId is the request's X-Request-Id
// (httpx.NewMetaFrom), the id the access and error logs carry (task 182 / F4.2).
func newOpenAPIMeta(ctx context.Context) openapi.Meta {
	m := httpx.NewMetaFrom(ctx)
	id := types.UUID(m.RequestID)
	return openapi.Meta{RequestId: &id, Timestamp: &m.Timestamp}
}

func dateOf(t time.Time) types.Date {
	return types.Date{Time: t}
}
