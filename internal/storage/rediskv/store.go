// Package rediskv implements storage.Store on top of Redis.
package rediskv

import (
	"context"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"

	"github.com/pablo-martins/500mb-club-go/internal/domain"
	"github.com/pablo-martins/500mb-club-go/internal/storage"
)

// DefaultMaxPointsPerDevice bounds the number of points retained per
// device sorted set. This is a deliberate trade-off: the challenge caps
// the whole stack at 500 MB, and an unbounded retention window would let
// long-running endurance/capacity load scenarios grow memory without
// limit. Old points beyond the cap are trimmed after each write.
const DefaultMaxPointsPerDevice = 5000

// Store implements storage.Store using one Redis sorted set per device,
// keyed by "dev:{id}", with member = binary-encoded point and score = ts
// (epoch millis). This gives O(log N) inserts and O(log N + M) range
// queries and top-K reads via ZSET rank/score operations.
type Store struct {
	client             *redis.Client
	maxPointsPerDevice int64
}

// New creates a Store backed by an already-configured *redis.Client.
// maxPointsPerDevice <= 0 falls back to DefaultMaxPointsPerDevice.
func New(client *redis.Client, maxPointsPerDevice int64) *Store {
	if maxPointsPerDevice <= 0 {
		maxPointsPerDevice = DefaultMaxPointsPerDevice
	}
	return &Store{client: client, maxPointsPerDevice: maxPointsPerDevice}
}

var _ storage.Store = (*Store)(nil)

func key(id string) string {
	return "dev:" + id
}

// Ping reports whether Redis is reachable.
func (s *Store) Ping(ctx context.Context) error {
	if err := s.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("%w: %v", storage.ErrUnavailable, err)
	}
	return nil
}

// Close closes the underlying Redis client.
func (s *Store) Close() error {
	return s.client.Close()
}

// InsertPoint persists a single point and trims the device's history to
// maxPointsPerDevice.
func (s *Store) InsertPoint(ctx context.Context, id string, p domain.Point) error {
	k := key(id)
	member := EncodePoint(p)

	pipe := s.client.TxPipeline()
	pipe.ZAdd(ctx, k, redis.Z{Score: float64(p.TS), Member: member})
	card := pipe.ZCard(ctx, k)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("rediskv: insert point: %w", err)
	}

	return s.trim(ctx, k, card.Val())
}

// InsertBatch persists multiple points in a single pipeline round trip
// and trims the device's history to maxPointsPerDevice. Returns the
// number of points accepted.
func (s *Store) InsertBatch(ctx context.Context, id string, points []domain.Point) (int, error) {
	if len(points) == 0 {
		return 0, nil
	}

	k := key(id)
	members := make([]redis.Z, len(points))
	for i, p := range points {
		members[i] = redis.Z{Score: float64(p.TS), Member: EncodePoint(p)}
	}

	pipe := s.client.TxPipeline()
	pipe.ZAdd(ctx, k, members...)
	card := pipe.ZCard(ctx, k)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, fmt.Errorf("rediskv: insert batch: %w", err)
	}

	if err := s.trim(ctx, k, card.Val()); err != nil {
		return 0, err
	}
	return len(points), nil
}

// trim removes the oldest members beyond maxPointsPerDevice, if any.
func (s *Store) trim(ctx context.Context, k string, count int64) error {
	stop := count - s.maxPointsPerDevice - 1
	if stop < 0 {
		return nil // within budget, nothing to trim
	}
	if err := s.client.ZRemRangeByRank(ctx, k, 0, stop).Err(); err != nil {
		return fmt.Errorf("rediskv: trim: %w", err)
	}
	return nil
}

// QueryRange returns points for device id with ts in [from, to], ordered
// ascending by ts, paginated via an exclusive-lower-bound cursor (the ts
// of the last point of the previous page).
func (s *Store) QueryRange(ctx context.Context, id string, from, to int64, limit int, cursor string) (storage.Page, error) {
	minScore := "(" + strconv.FormatInt(from-1, 10) // inclusive `from` via exclusive from-1
	if from <= 0 {
		minScore = strconv.FormatInt(from, 10)
	}
	if cursor != "" {
		cursorTS, err := strconv.ParseInt(cursor, 10, 64)
		if err != nil {
			return storage.Page{}, storage.ErrInvalidCursor
		}
		minScore = "(" + strconv.FormatInt(cursorTS, 10)
	}
	maxScore := strconv.FormatInt(to, 10)

	res, err := s.client.ZRangeArgs(ctx, redis.ZRangeArgs{
		Key:     key(id),
		Start:   minScore,
		Stop:    maxScore,
		ByScore: true,
		Offset:  0,
		Count:   int64(limit) + 1,
	}).Result()
	if err != nil {
		return storage.Page{}, fmt.Errorf("rediskv: query range: %w", err)
	}

	hasMore := len(res) > limit
	if hasMore {
		res = res[:limit]
	}

	points := make([]domain.Point, 0, len(res))
	for _, member := range res {
		p, decErr := DecodePoint([]byte(member))
		if decErr != nil {
			continue // corrupted member; skip rather than fail the whole page
		}
		points = append(points, p)
	}

	page := storage.Page{Points: points}
	if hasMore && len(points) > 0 {
		page.NextCursor = strconv.FormatInt(points[len(points)-1].TS, 10)
	}
	return page, nil
}

// RecentWindow returns up to domain.AnomalyWindow most recent points for
// device id, ordered oldest-to-newest.
func (s *Store) RecentWindow(ctx context.Context, id string) ([]domain.Point, error) {
	res, err := s.client.ZRange(ctx, key(id), -domain.AnomalyWindow, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("rediskv: recent window: %w", err)
	}

	points := make([]domain.Point, 0, len(res))
	for _, member := range res {
		p, decErr := DecodePoint([]byte(member))
		if decErr != nil {
			continue
		}
		points = append(points, p)
	}
	return points, nil
}
