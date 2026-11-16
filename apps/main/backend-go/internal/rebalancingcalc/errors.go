package rebalancingcalc

// UnavailableError is the calculation's one failure mode: a price or FX rate it needs is not
// available. It knows nothing about HTTP (task-174 F1.5, option A: httpx.AppError stays the
// app-wide error model, but this pure-calculation package does not construct HTTP errors);
// httpx.WriteError maps any error whose Unavailable() reports true to 503 SERVICE_UNAVAILABLE
// with Error() as the message, so the wire answer is unchanged.
type UnavailableError struct{ msg string }

func (e *UnavailableError) Error() string { return e.msg }

// Unavailable marks the error as "a dependency is unavailable" (behaviour, not type, is what
// httpx asserts).
func (e *UnavailableError) Unavailable() bool { return true }

func fxRateUnavailable(currency string) *UnavailableError {
	return &UnavailableError{msg: "FX rate unavailable for currency: " + currency}
}
