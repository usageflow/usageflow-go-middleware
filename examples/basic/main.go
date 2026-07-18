package main

import (
	"context"
	"log"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/usageflow/usageflow-go-middleware/v2/pkg/middleware"
	"github.com/usageflow/usageflow-go-middleware/v2/pkg/tracker"
)

// listUsers is ordinary application code (no UsageFlow imports).
// Build with `usageflow go build` so compile-time instrumentation records it
// on the request call chain when called with c.Request.Context().
func listUsers(ctx context.Context) ([]string, error) {
	_ = ctx
	return []string{"user1", "user2"}, nil
}

func main() {
	r := gin.Default()

	uf := middleware.New(os.Getenv("USAGEFLOW_API_KEY"))
	r.Use(uf.RequestInterceptor())

	r.GET("/api/go/users", func(c *gin.Context) {
		users, err := listUsers(c.Request.Context())
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{
			"message": "Hello Users!",
			"users":   users,
		})
	})

	// Local instrumentation demo: listUsers is captured automatically by the
	// build-time rewriter. No Track/Wrap calls are needed in business code.
	r.GET("/api/go/instrumentation-demo", func(c *gin.Context) {
		users, err := listUsers(c.Request.Context())
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		c.JSON(200, gin.H{
			"message":   "Automatic instrumentation is working",
			"users":     users,
			"callChain": tracker.GetCallChain(c.Request.Context()),
		})
	})

	r.GET("/api/go/users/:userId", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "Hello User!",
			"user":    c.Param("userId"),
		})
	})

	r.POST("/api/go/data", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "Data received",
			"status":  "success",
		})
	})

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "healthy"})
	})

	if err := r.Run(":8080"); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}
