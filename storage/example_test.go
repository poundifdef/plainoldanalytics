package storage_test

import (
	"fmt"

	"jaygoel.com/plainoldanalytics/storage"
)

// ExampleNewConfig builds the configuration shared by the capture
// middleware and the dashboard. WithUserKey marks which context-value name
// the UI treats as "the user"; handlers attach it (and any other
// dimensions) with capture.Set — no registration needed.
func ExampleNewConfig() {
	cfg := storage.NewConfig(storage.WithUserKey("account_id"))
	fmt.Println(cfg.UserKey)
	// Output:
	// account_id
}
