package rediskv

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablo-martins/500mb-club-go/internal/domain"
	"github.com/pablo-martins/500mb-club-go/internal/storage"
)

// newTestStore spins up an in-memory Redis (miniredis) and returns a Store
// wired to it, plus a cleanup func.
func newTestStore(t *testing.T, maxPointsPerDevice int64) (*Store, func()) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := New(client, maxPointsPerDevice)

	cleanup := func() {
		_ = store.Close()
		mr.Close()
	}
	return store, cleanup
}

func pointAt(ts int64) domain.Point {
	return domain.Point{TS: ts, Lat: 1, Lon: 2, AX: 0.1, AY: 0.2, AZ: 9.8}
}

func TestStore_Ping(t *testing.T) {
	store, cleanup := newTestStore(t, 0)
	defer cleanup()

	err := store.Ping(context.Background())
	assert.NoError(t, err)
}

func TestStore_Ping_Unreachable(t *testing.T) {
	client := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1", // nothing listening
		MaxRetries:  0,
		DialTimeout: 200 * time.Millisecond,
	})
	store := New(client, 0)
	defer store.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	err := store.Ping(ctx)
	assert.ErrorIs(t, err, storage.ErrUnavailable)
}

func TestStore_InsertPoint_And_QueryRange(t *testing.T) {
	store, cleanup := newTestStore(t, 0)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, store.InsertPoint(ctx, "dev-1", pointAt(100)))
	require.NoError(t, store.InsertPoint(ctx, "dev-1", pointAt(200)))
	require.NoError(t, store.InsertPoint(ctx, "dev-1", pointAt(300)))

	page, err := store.QueryRange(ctx, "dev-1", 0, 1000, 100, "")
	require.NoError(t, err)
	require.Len(t, page.Points, 3)
	assert.Equal(t, int64(100), page.Points[0].TS)
	assert.Equal(t, int64(200), page.Points[1].TS)
	assert.Equal(t, int64(300), page.Points[2].TS)
	assert.Empty(t, page.NextCursor)
}

func TestStore_QueryRange_RespectsFromTo(t *testing.T) {
	store, cleanup := newTestStore(t, 0)
	defer cleanup()
	ctx := context.Background()

	for _, ts := range []int64{100, 200, 300, 400, 500} {
		require.NoError(t, store.InsertPoint(ctx, "dev-1", pointAt(ts)))
	}

	page, err := store.QueryRange(ctx, "dev-1", 200, 400, 100, "")
	require.NoError(t, err)
	require.Len(t, page.Points, 3)
	assert.Equal(t, int64(200), page.Points[0].TS)
	assert.Equal(t, int64(400), page.Points[2].TS)
}

func TestStore_QueryRange_InclusiveBoundaries(t *testing.T) {
	store, cleanup := newTestStore(t, 0)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, store.InsertPoint(ctx, "dev-1", pointAt(100)))
	require.NoError(t, store.InsertPoint(ctx, "dev-1", pointAt(200)))

	page, err := store.QueryRange(ctx, "dev-1", 100, 200, 100, "")
	require.NoError(t, err)
	assert.Len(t, page.Points, 2, "both boundary points must be included")
}

func TestStore_QueryRange_Pagination(t *testing.T) {
	store, cleanup := newTestStore(t, 0)
	defer cleanup()
	ctx := context.Background()

	for i := int64(0); i < 25; i++ {
		require.NoError(t, store.InsertPoint(ctx, "dev-1", pointAt(i*10)))
	}

	var allPoints []domain.Point
	cursor := ""
	for i := 0; i < 10; i++ { // safety bound on iterations
		page, err := store.QueryRange(ctx, "dev-1", 0, 1000, 10, cursor)
		require.NoError(t, err)
		allPoints = append(allPoints, page.Points...)
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}

	require.Len(t, allPoints, 25)
	for i, p := range allPoints {
		assert.Equal(t, int64(i*10), p.TS)
	}
}

func TestStore_QueryRange_EmptyDeviceReturnsEmptyPage(t *testing.T) {
	store, cleanup := newTestStore(t, 0)
	defer cleanup()

	page, err := store.QueryRange(context.Background(), "unknown-device", 0, 1000, 100, "")
	require.NoError(t, err)
	assert.Empty(t, page.Points)
	assert.Empty(t, page.NextCursor)
}

func TestStore_QueryRange_InvalidCursor(t *testing.T) {
	store, cleanup := newTestStore(t, 0)
	defer cleanup()

	_, err := store.QueryRange(context.Background(), "dev-1", 0, 1000, 100, "not-a-number")
	assert.ErrorIs(t, err, storage.ErrInvalidCursor)
}

func TestStore_InsertBatch(t *testing.T) {
	store, cleanup := newTestStore(t, 0)
	defer cleanup()
	ctx := context.Background()

	points := []domain.Point{pointAt(1), pointAt(2), pointAt(3)}
	n, err := store.InsertBatch(ctx, "dev-1", points)
	require.NoError(t, err)
	assert.Equal(t, 3, n)

	page, err := store.QueryRange(ctx, "dev-1", 0, 1000, 100, "")
	require.NoError(t, err)
	assert.Len(t, page.Points, 3)
}

func TestStore_InsertBatch_Empty(t *testing.T) {
	store, cleanup := newTestStore(t, 0)
	defer cleanup()

	n, err := store.InsertBatch(context.Background(), "dev-1", nil)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestStore_RecentWindow_OrderedOldestToNewest(t *testing.T) {
	store, cleanup := newTestStore(t, 0)
	defer cleanup()
	ctx := context.Background()

	for i := int64(1); i <= 10; i++ {
		require.NoError(t, store.InsertPoint(ctx, "dev-1", pointAt(i)))
	}

	window, err := store.RecentWindow(ctx, "dev-1")
	require.NoError(t, err)
	require.Len(t, window, 10)
	assert.Equal(t, int64(1), window[0].TS)
	assert.Equal(t, int64(10), window[len(window)-1].TS)
}

func TestStore_RecentWindow_CappedAtAnomalyWindow(t *testing.T) {
	store, cleanup := newTestStore(t, 0) // 0 => DefaultMaxPointsPerDevice, larger than AnomalyWindow
	defer cleanup()
	ctx := context.Background()

	total := domain.AnomalyWindow + 50
	for i := int64(0); i < int64(total); i++ {
		require.NoError(t, store.InsertPoint(ctx, "dev-1", pointAt(i)))
	}

	window, err := store.RecentWindow(ctx, "dev-1")
	require.NoError(t, err)
	require.Len(t, window, domain.AnomalyWindow)
	// The window must hold the *most recent* AnomalyWindow points.
	wantFirstTS := int64(total - domain.AnomalyWindow)
	assert.Equal(t, wantFirstTS, window[0].TS)
	assert.Equal(t, int64(total-1), window[len(window)-1].TS)
}

func TestStore_RecentWindow_EmptyDevice(t *testing.T) {
	store, cleanup := newTestStore(t, 0)
	defer cleanup()

	window, err := store.RecentWindow(context.Background(), "unknown-device")
	require.NoError(t, err)
	assert.Empty(t, window)
}

func TestStore_TrimsOldPointsBeyondMax(t *testing.T) {
	const maxPoints = 5
	store, cleanup := newTestStore(t, maxPoints)
	defer cleanup()
	ctx := context.Background()

	for i := int64(0); i < 20; i++ {
		require.NoError(t, store.InsertPoint(ctx, "dev-1", pointAt(i)))
	}

	page, err := store.QueryRange(ctx, "dev-1", 0, 1000, 100, "")
	require.NoError(t, err)
	require.Len(t, page.Points, maxPoints, "history must be trimmed to maxPointsPerDevice")
	// The retained points must be the most recent ones (15..19).
	assert.Equal(t, int64(15), page.Points[0].TS)
	assert.Equal(t, int64(19), page.Points[maxPoints-1].TS)
}

func TestStore_TrimsOldPointsBeyondMax_Batch(t *testing.T) {
	const maxPoints = 5
	store, cleanup := newTestStore(t, maxPoints)
	defer cleanup()
	ctx := context.Background()

	points := make([]domain.Point, 20)
	for i := range points {
		points[i] = pointAt(int64(i))
	}
	n, err := store.InsertBatch(ctx, "dev-1", points)
	require.NoError(t, err)
	assert.Equal(t, 20, n)

	page, err := store.QueryRange(ctx, "dev-1", 0, 1000, 100, "")
	require.NoError(t, err)
	require.Len(t, page.Points, maxPoints)
}

func TestStore_DevicesAreIsolated(t *testing.T) {
	store, cleanup := newTestStore(t, 0)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, store.InsertPoint(ctx, "dev-a", pointAt(1)))
	require.NoError(t, store.InsertPoint(ctx, "dev-b", pointAt(2)))

	pageA, err := store.QueryRange(ctx, "dev-a", 0, 1000, 100, "")
	require.NoError(t, err)
	assert.Len(t, pageA.Points, 1)

	pageB, err := store.QueryRange(ctx, "dev-b", 0, 1000, 100, "")
	require.NoError(t, err)
	assert.Len(t, pageB.Points, 1)
}
