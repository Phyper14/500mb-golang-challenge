// Package storage defines the persistence contract used by the HTTP
// handlers to ingest and query telemetry points, decoupled from any
// specific engine (the reference implementation uses Redis).
package storage

import (
	"context"
	"errors"

	"github.com/Phyper14/500mb-golang-challenge/internal/domain"
)

// ErrUnavailable is returned by Ping when the backing store cannot be
// reached; handlers translate this into a 503 on /readyz.
var ErrUnavailable = errors.New("storage: unavailable")

// ErrInvalidCursor is returned by QueryRange when the supplied cursor is
// not a value produced by a previous call (handlers translate this to a
// 400 Bad Request).
var ErrInvalidCursor = errors.New("storage: invalid cursor")

// Page is the result of a windowed telemetry query.
type Page struct {
	Points     []domain.Point
	NextCursor string // empty when there are no more pages
}

// Store is the persistence contract for telemetry points.
//
// Implementations must be safe for concurrent use by multiple goroutines.
type Store interface {
	// Ping reports whether the store is reachable. Used by /readyz.
	Ping(ctx context.Context) error

	// InsertPoint persists a single point for device id.
	InsertPoint(ctx context.Context, id string, p domain.Point) error

	// InsertBatch persists multiple points for device id in one round
	// trip. Returns the number of points accepted.
	InsertBatch(ctx context.Context, id string, points []domain.Point) (int, error)

	// QueryRange returns points for device id with ts in [from, to]
	// (inclusive), ordered by ts ascending, paginated by cursor/limit.
	QueryRange(ctx context.Context, id string, from, to int64, limit int, cursor string) (Page, error)

	// RecentWindow returns up to domain.AnomalyWindow most recent points
	// for device id, ordered oldest-to-newest (so the last element is the
	// most recent point).
	RecentWindow(ctx context.Context, id string) ([]domain.Point, error)

	// Close releases underlying resources (connections, pools).
	Close() error
}
