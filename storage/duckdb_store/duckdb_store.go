// Package duckdb is the production Storage backend: request traffic is
// written through DuckDB's Appender API (with a periodic background flush),
// custom events and rrweb replay chunks through plain inserts, and sessions
// are derived at query time with a window function over each visitor's
// activity timeline.
//
// The caller owns the connector, so file vs in-memory databases and any
// DuckDB settings are decided at the call site; storage/memory wraps this
// package for a zero-config in-memory database.
package duckdb_store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/duckdb/duckdb-go/v2"
	"jaygoel.com/plainoldanalytics/storage"
)

var createTables = []string{
	`CREATE TABLE IF NOT EXISTS traffic (
		method       VARCHAR,
		pattern      VARCHAR,
		query_params MAP(VARCHAR, VARCHAR[]),
		path_params  MAP(VARCHAR, VARCHAR),
		status       INTEGER,
		size         BIGINT,
		duration     INTERVAL,
		ip           VARCHAR,
		ts           TIMESTAMP,
		headers      MAP(VARCHAR, VARCHAR[]),
		id           UBIGINT,
		visitor      UBIGINT,
		context      MAP(VARCHAR, VARCHAR),
		session      UBIGINT
	)`,
	`CREATE TABLE IF NOT EXISTS events (
		id      UBIGINT,
		ts      TIMESTAMP,
		name    VARCHAR,
		props   VARCHAR,
		visitor UBIGINT,
		session UBIGINT,
		path    VARCHAR,
		ip      VARCHAR
	)`,
	`CREATE TABLE IF NOT EXISTS replays (
		id      UBIGINT,
		ts      TIMESTAMP,
		visitor UBIGINT,
		session UBIGINT,
		data    VARCHAR
	)`,
}

const selectTraffic = `SELECT method, pattern, query_params, path_params,
	status, size, duration, ip, ts, headers, id, visitor, session, context
	FROM traffic`

// flushInterval bounds how long an appended row can sit in the appender's
// in-memory buffer before it is committed to the database.
const flushInterval = time.Second

// DuckDB is a Storage implementation over a single duckdb database: request
// traffic goes through the Appender API, custom events and replay chunks
// through plain inserts. Safe for concurrent use. Callers must Close it on
// shutdown or buffered traffic rows are lost.
type DuckDB struct {
	db       *sql.DB
	conn     driver.Conn
	mu       sync.Mutex
	appender *duckdb.Appender
	stop     chan struct{}
	stopOnce sync.Once
}

var _ storage.Storage = (*DuckDB)(nil)

// New creates the tables on the caller-provided connector and starts
// recording to it. The caller owns the connector's configuration (file vs
// in-memory, duckdb settings, boot queries); it must be a go-duckdb
// connector because the Appender API needs a raw duckdb connection, which a
// generic database/sql handle cannot provide.
func New(connector *duckdb.Connector) (*DuckDB, error) {
	db := sql.OpenDB(connector)
	for _, ddl := range createTables {
		if _, err := db.Exec(ddl); err != nil {
			db.Close()
			return nil, fmt.Errorf("duckdb create table: %w", err)
		}
	}
	// The appender needs its own raw connection; the appended rows are still
	// visible to queries made through db, which shares the same database.
	conn, err := connector.Connect(context.Background())
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("duckdb appender connection: %w", err)
	}
	appender, err := duckdb.NewAppenderFromConn(conn, "", "traffic")
	if err != nil {
		conn.Close()
		db.Close()
		return nil, fmt.Errorf("duckdb appender: %w", err)
	}
	d := &DuckDB{db: db, conn: conn, appender: appender, stop: make(chan struct{})}
	go d.flushLoop()
	return d, nil
}

// flushLoop periodically commits buffered rows so they reach the database
// even under low traffic; without it rows would only surface when the
// appender's internal chunk (~2048 rows) fills or on Close.
func (d *DuckDB) flushLoop() {
	t := time.NewTicker(flushInterval)
	defer t.Stop()
	for {
		select {
		case <-d.stop:
			return
		case <-t.C:
			if err := d.Flush(); err != nil {
				slog.Error("duckdb flush", "error", err)
			}
		}
	}
}

func (d *DuckDB) Record(r *http.Request, c storage.Capture) {
	d.mu.Lock()
	err := d.appender.AppendRow(
		r.Method,
		c.Pattern,
		toMap(r.URL.Query()),
		toMap(c.PathValues),
		int32(c.Status),
		int64(c.Size),
		duckdb.Interval{Micros: c.Duration.Microseconds()},
		storage.ClientIP(r),
		c.Time,
		toMap(map[string][]string(r.Header)),
		storage.NewID(),
		c.Visitor,
		toMap(c.Context),
		c.Session,
	)
	d.mu.Unlock()
	if err != nil {
		slog.Error("duckdb append", "error", err)
	}
}

// Identify converts all earlier anonymous traffic for a persistent browser
// identity. Once that browser has ever been labelled, it never changes old
// rows again, so a later login by someone else cannot rewrite its history.
func (d *DuckDB) Identify(ctx context.Context, visitor uint64, userKey, user string) error {
	if visitor == 0 || userKey == "" || user == "" {
		return nil
	}
	if err := d.Flush(); err != nil {
		return fmt.Errorf("duckdb flush: %w", err)
	}
	var known int
	if err := d.db.QueryRowContext(ctx,
		`SELECT count(*) FROM traffic WHERE visitor = ? AND coalesce(context[?], '') <> ''`,
		visitor, userKey).Scan(&known); err != nil {
		return fmt.Errorf("duckdb identify lookup: %w", err)
	}
	if known != 0 {
		return nil
	}
	_, err := d.db.ExecContext(ctx, `
		UPDATE traffic
		SET context = CASE WHEN context IS NULL THEN map([?], [?])
			ELSE map_concat(context, map([?], [?])) END
		WHERE visitor = ? AND coalesce(context[?], '') = ''`,
		userKey, user, userKey, user, visitor, userKey)
	if err != nil {
		return fmt.Errorf("duckdb identify: %w", err)
	}
	return nil
}

// Traffic implements storage.Storage. It flushes pending appends first so
// just-recorded traffic is included.
func (d *DuckDB) Traffic(ctx context.Context, f storage.TrafficFilter) ([]storage.Traffic, error) {
	if err := d.Flush(); err != nil {
		return nil, fmt.Errorf("duckdb flush: %w", err)
	}
	rows, err := d.db.QueryContext(ctx, selectTraffic+`
		WHERE (? = '' OR pattern LIKE '%' || ? || '%')
		AND (? = 0 OR status = ?)
		AND (? = '' OR context[?] = ?)
		AND (? = 0 OR visitor = ?)
		AND (? OR ts >= ?) AND (? OR ts <= ?)
		ORDER BY ts DESC LIMIT 500`,
		f.Pattern, f.Pattern, f.Status, f.Status,
		f.ContextKey, f.ContextKey, f.ContextValue, f.Visitor, f.Visitor,
		f.From.IsZero(), f.From, f.To.IsZero(), f.To)
	if err != nil {
		return nil, fmt.Errorf("duckdb query traffic: %w", err)
	}
	defer rows.Close()

	traffic := []storage.Traffic{}
	for rows.Next() {
		t, err := scanTraffic(rows)
		if err != nil {
			return nil, err
		}
		traffic = append(traffic, t)
	}
	return traffic, rows.Err()
}

// TrafficByID implements storage.Storage.
func (d *DuckDB) TrafficByID(ctx context.Context, id uint64) (storage.Traffic, error) {
	if err := d.Flush(); err != nil {
		return storage.Traffic{}, fmt.Errorf("duckdb flush: %w", err)
	}
	rows, err := d.db.QueryContext(ctx, selectTraffic+` WHERE id = ?`, id)
	if err != nil {
		return storage.Traffic{}, fmt.Errorf("duckdb query traffic: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return storage.Traffic{}, err
		}
		return storage.Traffic{}, storage.ErrNotFound
	}
	return scanTraffic(rows)
}

// TrafficSeries implements storage.Storage.
func (d *DuckDB) TrafficSeries(ctx context.Context, since time.Time, bucketMinutes int) ([]storage.SeriesPoint, error) {
	if err := d.Flush(); err != nil {
		return nil, fmt.Errorf("duckdb flush: %w", err)
	}
	rows, err := d.db.QueryContext(ctx,
		`SELECT time_bucket(to_minutes(CAST(? AS BIGINT)), ts) AS bucket, count(*)
		FROM traffic WHERE ts >= ? GROUP BY bucket ORDER BY bucket`,
		bucketMinutes, since)
	if err != nil {
		return nil, fmt.Errorf("duckdb query series: %w", err)
	}
	defer rows.Close()
	points := []storage.SeriesPoint{}
	for rows.Next() {
		var p storage.SeriesPoint
		if err := rows.Scan(&p.Bucket, &p.Count); err != nil {
			return nil, fmt.Errorf("duckdb scan series: %w", err)
		}
		points = append(points, p)
	}
	return points, rows.Err()
}

// TrafficStats implements storage.Storage. Referrers and user agents are
// returned as raw header values; the dashboard groups them for display.
// Wider raw limits than the displayed top-10 leave the presentation layer
// room to collapse rows (referrer URLs to hosts, user agents to browsers)
// without losing counts.
func (d *DuckDB) TrafficStats(ctx context.Context, since time.Time, userKey string) (storage.TrafficStats, error) {
	if err := d.Flush(); err != nil {
		return storage.TrafficStats{}, fmt.Errorf("duckdb flush: %w", err)
	}
	var s storage.TrafficStats
	err := d.db.QueryRowContext(ctx,
		`SELECT count(*), count(DISTINCT visitor) FILTER (coalesce(visitor, 0) <> 0)
		FROM traffic WHERE ts >= ?`, since).Scan(&s.Views, &s.Visitors)
	if err != nil {
		return storage.TrafficStats{}, fmt.Errorf("duckdb query stats: %w", err)
	}
	if s.Pages, err = d.nameCounts(ctx,
		`SELECT pattern, count(*) AS c FROM traffic WHERE ts >= ?
		GROUP BY pattern ORDER BY c DESC, pattern LIMIT 10`, since); err != nil {
		return storage.TrafficStats{}, err
	}
	if s.Referrers, err = d.nameCounts(ctx,
		`SELECT headers['Referer'][1] AS r, count(*) AS c FROM traffic
		WHERE ts >= ? AND coalesce(r, '') <> ''
		GROUP BY r ORDER BY c DESC, r LIMIT 100`, since); err != nil {
		return storage.TrafficStats{}, err
	}
	if s.UserAgents, err = d.nameCounts(ctx,
		`SELECT headers['User-Agent'][1] AS ua, count(*) AS c FROM traffic
		WHERE ts >= ? AND coalesce(ua, '') <> ''
		GROUP BY ua ORDER BY c DESC, ua LIMIT 100`, since); err != nil {
		return storage.TrafficStats{}, err
	}
	if userKey != "" {
		if s.Users, err = d.nameCounts(ctx,
			`SELECT context[?] AS u, count(*) AS c FROM traffic
			WHERE ts >= ? AND coalesce(u, '') <> ''
			GROUP BY u ORDER BY c DESC, u LIMIT 10`, userKey, since); err != nil {
			return storage.TrafficStats{}, err
		}
	}
	return s, nil
}

// nameCounts runs a (name, count) aggregation query into a NameCount list.
func (d *DuckDB) nameCounts(ctx context.Context, query string, args ...any) ([]storage.NameCount, error) {
	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("duckdb query stats: %w", err)
	}
	defer rows.Close()
	counts := []storage.NameCount{}
	for rows.Next() {
		var nc storage.NameCount
		if err := rows.Scan(&nc.Name, &nc.Count); err != nil {
			return nil, fmt.Errorf("duckdb scan stats: %w", err)
		}
		counts = append(counts, nc)
	}
	return counts, rows.Err()
}

// TrackEvent implements storage.Storage.
func (d *DuckDB) TrackEvent(ctx context.Context, e storage.Event) error {
	if e.ID == 0 {
		e.ID = storage.NewID()
	}
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	props, err := json.Marshal(e.Props)
	if err != nil {
		return fmt.Errorf("duckdb marshal props: %w", err)
	}
	_, err = d.db.ExecContext(ctx,
		`INSERT INTO events (id, ts, name, props, visitor, session, path, ip) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.Time, e.Name, string(props), e.Visitor, e.Session, e.Path, e.IP)
	if err != nil {
		return fmt.Errorf("duckdb insert event: %w", err)
	}
	return nil
}

// Events implements storage.Storage.
func (d *DuckDB) Events(ctx context.Context, f storage.EventFilter) ([]storage.Event, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, ts, name, props, visitor, session, path, ip FROM events
		WHERE (? = '' OR name = ?)
		AND (? = 0 OR visitor = ?)
		AND (? OR ts >= ?) AND (? OR ts <= ?)
		ORDER BY ts DESC LIMIT 500`,
		f.Name, f.Name, f.Visitor, f.Visitor,
		f.From.IsZero(), f.From, f.To.IsZero(), f.To)
	if err != nil {
		return nil, fmt.Errorf("duckdb query events: %w", err)
	}
	defer rows.Close()
	events := []storage.Event{}
	for rows.Next() {
		var e storage.Event
		var props string
		if err := rows.Scan(&e.ID, &e.Time, &e.Name, &props, &e.Visitor, &e.Session, &e.Path, &e.IP); err != nil {
			return nil, fmt.Errorf("duckdb scan event: %w", err)
		}
		if props != "" {
			if err := json.Unmarshal([]byte(props), &e.Props); err != nil {
				e.Props = map[string]any{"_raw": props}
			}
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// SaveReplay implements storage.Storage.
func (d *DuckDB) SaveReplay(ctx context.Context, c storage.ReplayChunk) error {
	if c.ID == 0 {
		c.ID = storage.NewID()
	}
	if c.Time.IsZero() {
		c.Time = time.Now()
	}
	_, err := d.db.ExecContext(ctx,
		`INSERT INTO replays (id, ts, visitor, session, data) VALUES (?, ?, ?, ?, ?)`,
		c.ID, c.Time, c.Visitor, c.Session, string(c.Data))
	if err != nil {
		return fmt.Errorf("duckdb insert replay: %w", err)
	}
	return nil
}

// Replay implements storage.Storage.
func (d *DuckDB) Replay(ctx context.Context, visitor uint64, from, to time.Time) ([]storage.ReplayChunk, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, ts, visitor, session, data FROM replays WHERE visitor = ?
		AND (? OR ts >= ?) AND (? OR ts <= ?)
		ORDER BY ts, id`, visitor,
		from.IsZero(), from, to.IsZero(), to)
	if err != nil {
		return nil, fmt.Errorf("duckdb query replays: %w", err)
	}
	defer rows.Close()
	chunks := []storage.ReplayChunk{}
	for rows.Next() {
		var c storage.ReplayChunk
		var data string
		if err := rows.Scan(&c.ID, &c.Time, &c.Visitor, &c.Session, &data); err != nil {
			return nil, fmt.Errorf("duckdb scan replay: %w", err)
		}
		c.Data = []byte(data)
		chunks = append(chunks, c)
	}
	return chunks, rows.Err()
}

// Sessions implements storage.Storage. Browser session IDs come from a
// session-only cookie, so a browser close ends the session without guessing
// from an inactivity timeout.
func (d *DuckDB) Sessions(ctx context.Context, f storage.SessionFilter) ([]storage.Session, error) {
	if err := d.Flush(); err != nil {
		return nil, fmt.Errorf("duckdb flush: %w", err)
	}
	rows, err := d.db.QueryContext(ctx, `
		WITH activity AS (
			SELECT visitor, session, ts, 1 AS is_traffic, 0 AS is_event, 0 AS is_replay,
				'' AS name, (? <> '' AND context[?] = ?) AS ctx_match,
				context[?] AS ann
				FROM traffic WHERE coalesce(visitor, 0) <> 0 AND coalesce(session, 0) <> 0
			UNION ALL
			SELECT visitor, session, ts, 0, 1, 0, name, false, NULL
				FROM events WHERE coalesce(visitor, 0) <> 0 AND coalesce(session, 0) <> 0
			UNION ALL
			SELECT visitor, session, ts, 0, 0, 1, '', false, NULL
				FROM replays WHERE coalesce(visitor, 0) <> 0 AND coalesce(session, 0) <> 0
		)
		SELECT visitor, session, min(ts), max(ts),
			count(*) FILTER (is_traffic = 1),
			count(*) FILTER (is_event = 1),
			count(*) FILTER (is_replay = 1) > 0,
			coalesce(max(ann) FILTER (coalesce(ann, '') <> ''), '')
		FROM activity
		WHERE (? = 0 OR visitor = ?)
		GROUP BY visitor, session
		HAVING (? = '' OR bool_or(ctx_match))
		AND (? = '' OR bool_or(name = ?))
		ORDER BY max(ts) DESC LIMIT 500`,
		f.ContextKey, f.ContextKey, f.ContextValue, f.AnnotateKey,
		f.Visitor, f.Visitor,
		f.ContextKey, f.EventName, f.EventName)
	if err != nil {
		return nil, fmt.Errorf("duckdb query sessions: %w", err)
	}
	defer rows.Close()
	sessions := []storage.Session{}
	for rows.Next() {
		var s storage.Session
		if err := rows.Scan(&s.Visitor, &s.ID, &s.Start, &s.End,
			&s.TrafficCount, &s.EventCount, &s.HasReplay, &s.Annotation); err != nil {
			return nil, fmt.Errorf("duckdb scan session: %w", err)
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

func scanTraffic(rows *sql.Rows) (storage.Traffic, error) {
	var t storage.Traffic
	var query, params, duration, headers, id, ctxMap any
	var ts sql.NullTime
	if err := rows.Scan(&t.Method, &t.Pattern, &query, &params,
		&t.Status, &t.Size, &duration, &t.IP, &ts, &headers, &id,
		&t.Visitor, &t.Session, &ctxMap); err != nil {
		return t, fmt.Errorf("duckdb scan traffic: %w", err)
	}
	if v, ok := id.(uint64); ok {
		t.ID = v
	}
	t.Time = ts.Time
	t.Context = toStringMap(ctxMap)
	t.Headers = toStringListMap(headers)
	t.QueryParams = toStringListMap(query)
	t.PathParams = toStringMap(params)
	if iv, ok := duration.(duckdb.Interval); ok {
		t.Duration = time.Duration(iv.Micros) * time.Microsecond
	}
	return t, nil
}

// toMap converts a plain Go map to the duckdb.Map the appender requires for
// MAP columns.
func toMap[V any](m map[string]V) duckdb.Map {
	out := make(duckdb.Map, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// toStringMap converts a scanned MAP(VARCHAR, VARCHAR) back to a Go map.
// Scanning a MAP column yields an OrderedMap, not the (deprecated) Map the
// appender accepts on the write side — the two directions use different
// types.
func toStringMap(v any) map[string]string {
	om, _ := v.(duckdb.OrderedMap)
	keys, vals := om.Keys(), om.Values()
	out := make(map[string]string, len(keys))
	for i, k := range keys {
		ks, kok := k.(string)
		vs, vok := vals[i].(string)
		if kok && vok {
			out[ks] = vs
		}
	}
	return out
}

// toStringListMap converts a scanned MAP(VARCHAR, VARCHAR[]) back to a Go map.
func toStringListMap(v any) map[string][]string {
	om, _ := v.(duckdb.OrderedMap)
	keys, vals := om.Keys(), om.Values()
	out := make(map[string][]string, len(keys))
	for i, k := range keys {
		ks, ok := k.(string)
		if !ok {
			continue
		}
		list, ok := vals[i].([]any)
		if !ok {
			continue
		}
		strs := make([]string, 0, len(list))
		for _, item := range list {
			if s, ok := item.(string); ok {
				strs = append(strs, s)
			}
		}
		out[ks] = strs
	}
	return out
}

// Flush makes all appended traffic rows visible to queries. The appender
// otherwise flushes on its own once its internal chunk fills up.
func (d *DuckDB) Flush() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.appender.Flush()
}

// Close flushes pending rows and releases the database.
func (d *DuckDB) Close() error {
	d.stopOnce.Do(func() { close(d.stop) })
	d.mu.Lock()
	defer d.mu.Unlock()
	err := d.appender.Close()
	if cerr := d.conn.Close(); err == nil {
		err = cerr
	}
	if cerr := d.db.Close(); err == nil {
		err = cerr
	}
	return err
}
