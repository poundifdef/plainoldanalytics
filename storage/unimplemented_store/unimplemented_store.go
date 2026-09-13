// Package unimplemented_store provides a no-op Storage: every method
// returns an empty result and no error. Embed Unimplemented in real
// implementations (and test fakes) that only care about a subset of the
// Storage interface, or use it directly to wire up plainoldanalytics
// without a real backend.
package unimplemented_store

import (
	"context"
	"net/http"
	"time"

	"jaygoel.com/plainoldanalytics/storage"
)

// Unimplemented provides no-op defaults for the full storage.Storage
// interface.
type Unimplemented struct{}

func (Unimplemented) Record(r *http.Request, c storage.Capture) {}
func (Unimplemented) Traffic(context.Context, storage.TrafficFilter) ([]storage.Traffic, error) {
	return []storage.Traffic{}, nil
}
func (Unimplemented) TrafficByID(context.Context, uint64) (storage.Traffic, error) {
	return storage.Traffic{}, storage.ErrNotFound
}
func (Unimplemented) TrafficSeries(context.Context, time.Time, int) ([]storage.SeriesPoint, error) {
	return []storage.SeriesPoint{}, nil
}
func (Unimplemented) TrafficStats(context.Context, time.Time, string) (storage.TrafficStats, error) {
	return storage.TrafficStats{}, nil
}
func (Unimplemented) TrackEvent(context.Context, storage.Event) error { return nil }
func (Unimplemented) Events(context.Context, storage.EventFilter) ([]storage.Event, error) {
	return []storage.Event{}, nil
}
func (Unimplemented) SaveReplay(context.Context, storage.ReplayChunk) error { return nil }
func (Unimplemented) Replay(context.Context, uint64, time.Time, time.Time) ([]storage.ReplayChunk, error) {
	return []storage.ReplayChunk{}, nil
}
func (Unimplemented) Sessions(context.Context, storage.SessionFilter) ([]storage.Session, error) {
	return []storage.Session{}, nil
}
func (Unimplemented) Close() error { return nil }
