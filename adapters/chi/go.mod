module jaygoel.com/plainoldanalytics/adapters/chi

go 1.26.4

require jaygoel.com/plainoldanalytics v0.0.0

require github.com/go-chi/chi/v5 v5.2.3

require (
	github.com/apache/arrow-go/v18 v18.5.1 // indirect
	github.com/duckdb/duckdb-go-bindings v0.10505.0 // indirect
	github.com/duckdb/duckdb-go-bindings/lib/darwin-amd64 v0.10505.0 // indirect
	github.com/duckdb/duckdb-go-bindings/lib/darwin-arm64 v0.10505.0 // indirect
	github.com/duckdb/duckdb-go-bindings/lib/linux-amd64 v0.10505.0 // indirect
	github.com/duckdb/duckdb-go-bindings/lib/linux-arm64 v0.10505.0 // indirect
	github.com/duckdb/duckdb-go-bindings/lib/windows-amd64 v0.10505.0 // indirect
	github.com/duckdb/duckdb-go/v2 v2.10505.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/goccy/go-json v0.10.5 // indirect
	github.com/google/flatbuffers v25.12.19+incompatible // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/klauspost/compress v1.18.3 // indirect
	github.com/klauspost/cpuid/v2 v2.3.0 // indirect
	github.com/pierrec/lz4/v4 v4.1.25 // indirect
	github.com/zeebo/xxh3 v1.1.0 // indirect
	golang.org/x/exp v0.0.0-20260112195511-716be5621a96 // indirect
	golang.org/x/mod v0.32.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	golang.org/x/telemetry v0.0.0-20260116145544-c6413dc483f5 // indirect
	golang.org/x/tools v0.41.0 // indirect
	golang.org/x/xerrors v0.0.0-20240903120638-7835f813f4da // indirect
	jaygoel.com/plainoldanalytics/adapters/http v0.0.0 // indirect
	jaygoel.com/plainoldanalytics/storage/duckdb_store v0.0.0 // indirect
	jaygoel.com/plainoldanalytics/webapp v0.0.0 // indirect
)

replace jaygoel.com/plainoldanalytics => ../..

replace jaygoel.com/plainoldanalytics/adapters/http => ../../adapters/http

replace jaygoel.com/plainoldanalytics/storage/duckdb_store => ../../storage/duckdb_store

replace jaygoel.com/plainoldanalytics/webapp => ../../webapp
