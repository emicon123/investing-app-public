// Package accountcash is portfolio-c's thin wiring for the account-cash domain: the whole
// behavior lives in the shared common-go/accountcash kernel (manual-update + post-purchase-deduct
// rules), and this package holds only what genuinely differs per app — the Store over the
// PK=portfolio row with the single balancePln column, plus the chi handler.
//
// Wire notes (binding, verified against this app's OpenAPI bundle):
//
//   - Cash answers WITHOUT the {data,meta} envelope — the raw AccountCashDto. The
//     handler uses httpx.WriteJSON directly.
//   - The DTO carries the extra portfolio field (portfolioB's AccountCashDto has none).
//   - balancePln is @NotNull: a missing field answers 400 (the handler checks body
//     presence explicitly — oapi-codegen decodes a missing required number as 0).
//     A negative amount answers 400; deduct floors at zero.
package accountcash
