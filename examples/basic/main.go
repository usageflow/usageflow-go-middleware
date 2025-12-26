package main

import (
	"log"

	"github.com/gin-gonic/gin"
	"github.com/usageflow/usageflow-go-middleware/v2/pkg/middleware"
)

func main() {
	// Initialize Gin
	r := gin.Default()

	// Initialize UsageFlow middleware
	uf := middleware.New("eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhY2NvdW50SWQiOiI2OTBjZWJkMzcyMTA1NjBjYzMzYmQwYjciLCJleHAiOjE3NjU5MDU3MTIsImtleUlkIjoiNjkzZGEwYjA0MjAxMzE3NGM3ODE0NGZmIiwicGVybWlzc2lvbnMiOlsiYWxsIl19.zeJo5PS0u-JKgJtnbpSZPRbgZyM8pIWjqA2ZR0qp4ZA")

	// Use the middleware
	r.Use(uf.RequestInterceptor())

	// Define your routes
	r.GET("/api/go/users", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "Hello Users!",
			"users":   []string{"user1", "user2"},
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
		c.JSON(200, gin.H{
			"status": "healthy",
		})
	})

	// Start the server
	if err := r.Run(":8080"); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}
