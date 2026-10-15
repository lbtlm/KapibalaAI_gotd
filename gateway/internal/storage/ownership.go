package storage

import "errors"

// ErrWriterLeaseLost requires a process restart before further gateway effects.
var ErrWriterLeaseLost = errors.New("gateway PostgreSQL writer lease lost")
