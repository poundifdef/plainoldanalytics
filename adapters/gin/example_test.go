package gin_test

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"jaygoel.com/plainoldanalytics/adapters/capture"
	ginadapter "jaygoel.com/plainoldanalytics/adapters/gin"
	"jaygoel.com/plainoldanalytics/storage"
	"jaygoel.com/plainoldanalytics/storage/unimplemented_store"
)

// Example instruments a gin application. unimplemented_store.Unimplemented
// stands in for a real backend; use storage/duckdb_store (or
// storage/memory_store) in a real application.
func Example() {
	var store storage.Storage = unimplemented_store.Unimplemented{}
	capturer := capture.New(store, storage.NewConfig())

	router := gin.New()
	router.Use(ginadapter.New(capturer).Handle)
	router.GET("/hello/:name", func(c *gin.Context) {
		capture.Set(c.Request.Context(), "name", c.Param("name"))
		capture.Set(c.Request.Context(), "plan", "pro")
		c.String(http.StatusOK, "hello "+c.Param("name"))
	})

	log.Fatal(router.Run(":8080"))
}
