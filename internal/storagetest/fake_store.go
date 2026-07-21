// Package storagetest provides a hand-written in-memory fake of
// storage.Store for unit-testing HTTP handlers without a real Redis.
package storagetest

import (
	"context"
	"sort"
	"strconv"
	"sync"

	"github.com/pablo-martins/500mb-club-go/internal/domain"
	"github.com/pablo-martins/500mb-club-go/internal/storage"
)

// FakeStore is a minimal, concurrency-safe in-memory implementation of
// storage.Store, intended only for tests.
type FakeStore struct {
	mu       sync.Mutex
	points   map[string][]domain.Point // device id -> points, kept sorted by ts
	pingErr  error
	failNext error // if set, the next mutating call returns this error once
}

// NewFakeStore returns an empty FakeStore ready to use.
func NewFakeStore() *FakeStore {
	return &FakeStore{points: make(map[string][]domain.Point)}
}

var _ storage.Store = (*FakeStore)(nil)

// SetPingError configures Ping to return err (nil to clear).
func (f *FakeStore) SetPingError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pingErr = err
}

// FailNextWrite makes the next InsertPoint/InsertBatch call return err
// instead of succeeding. Consumed after one use.
func (f *FakeStore) FailNextWrite(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failNext = err
}

// Ping implements storage.Store.
func (f *FakeStore) Ping(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pingErr
}

// Close implements storage.Store.
func (f *FakeStore) Close() error { return nil }

// InsertPoint implements storage.Store.
func (f *FakeStore) InsertPoint(_ context.Context, id string, p domain.Point) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNext != nil {
		err := f.failNext
		f.failNext = nil
		return err
	}
	f.insertLocked(id, p)
	return nil
}

// InsertBatch implements storage.Store.
func (f *FakeStore) InsertBatch(_ context.Context, id string, points []domain.Point) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNext != nil {
		err := f.failNext
		f.failNext = nil
		return 0, err
	}
	for _, p := range points {
		f.insertLocked(id, p)
	}
	return len(points), nil
}

func (f *FakeStore) insertLocked(id string, p domain.Point) {
	list := f.points[id]
	list = append(list, p)
	sort.Slice(list, func(i, j int) bool { return list[i].TS < list[j].TS })
	f.points[id] = list
}

// QueryRange implements storage.Store.
func (f *FakeStore) QueryRange(_ context.Context, id string, from, to int64, limit int, cursor string) (storage.Page, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	minTS := from
	if cursor != "" {
		c, err := strconv.ParseInt(cursor, 10, 64)
		if err != nil {
			return storage.Page{}, storage.ErrInvalidCursor
		}
		minTS = c + 1
	}

	var matched []domain.Point
	for _, p := range f.points[id] {
		if p.TS >= minTS && p.TS <= to {
			matched = append(matched, p)
		}
	}

	hasMore := len(matched) > limit
	if hasMore {
		matched = matched[:limit]
	}

	page := storage.Page{Points: matched}
	if hasMore && len(matched) > 0 {
		page.NextCursor = strconv.FormatInt(matched[len(matched)-1].TS, 10)
	}
	return page, nil
}

// RecentWindow implements storage.Store.
func (f *FakeStore) RecentWindow(_ context.Context, id string) ([]domain.Point, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	list := f.points[id]
	if len(list) <= domain.AnomalyWindow {
		out := make([]domain.Point, len(list))
		copy(out, list)
		return out, nil
	}
	start := len(list) - domain.AnomalyWindow
	out := make([]domain.Point, domain.AnomalyWindow)
	copy(out, list[start:])
	return out, nil
}
