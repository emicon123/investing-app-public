package accountcash

import "errors"

// ErrNotFound is returned by Store.Load when no snapshot row exists — the shared
// equivalent of the per-app sqlc "no rows" error, so this kernel never imports a
// concrete sqlc package. Each sibling's thin Store maps its own sqlc "no rows"
// error to this sentinel.
var ErrNotFound = errors.New("accountcash: snapshot not found")

func isNotFound(err error) bool { return errors.Is(err, ErrNotFound) }
