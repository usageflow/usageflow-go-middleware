package main

import (
	"log"

	"github.com/gin-gonic/gin"
	"github.com/usageflow/usageflow-go-middleware/pkg/config"
	"github.com/usageflow/usageflow-go-middleware/pkg/middleware"
)

func main() {
	// Initialize Gin
	r := gin.Default()

	// Initialize UsageFlow middleware
	uf := middleware.New("eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhY2NvdW50SWQiOiI2N2RkZTkzN2E4NDNjMmJhOTM3NzE3MmEiLCJleHAiOjE3NDI4NTY5MDUsImtleUlkIjoiNjdkZGVlNDlhODQzYzJiYTkzNzcxNzJmIiwicGVybWlzc2lvbnMiOlsiYWxsIl19.Q48PBbGlESjn2Sz5izlOGNPhko8o1yNy3lgquX9O9oM")

	// Define routes to monitor
	routes := []config.Route{
		{Method: "GET", URL: "/api/users"},
		{Method: "POST", URL: "/api/data"},
		{Method: "*", URL: "/api/v1/*"}, // Wildcard example
	}

	// Define whitelist routes (optional)
	whiteList := []config.Route{
		{Method: "GET", URL: "/health"},
		{Method: "GET", URL: "/metrics"},
	}

	// Use the middleware
	r.Use(uf.RequestInterceptor(routes, whiteList))

	// Define your routes
	r.GET("/api/users", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "Hello Users!",
			"users":   []string{"user1", "user2"},
		})
	})

	r.POST("/api/data", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "Data received",
			"status":  "success",
		})
	})

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status": "healthy",
		})
	})

	// Start the server
	if err := r.Run(":8080"); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}
