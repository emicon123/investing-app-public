// Package accountcash is portfolio-b's thin wiring for the account-cash domain: the whole
// behavior lives in the shared common-go/accountcash kernel (manual-update + post-purchase-deduct
// rules), and this package holds only what genuinely differs per app — the Store over the singleton
// id=1 row with four IKE/IKZE columns, plus the chi handler.
//
// Wire notes (binding, verified against this app's OpenAPI bundle):
//
//   - Cash answers WITHOUT the {data,meta} envelope — the raw AccountCashDto. The
//     handler uses httpx.WriteJSON directly.
//   - Reading the current balances for rebalancing is not part of this package
//     (internal/rebalancing reads the snapshot itself).
//   - A negative amount in PUT/POST-deduct answers 400; a missing (null) amount
//     counts as zero.
package accountcash
