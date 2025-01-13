package main

import (
	"github.com/gin-gonic/gin"
	"github.com/usageflow/usageflow-go-middleware/middlewares"
)

func main() {
	app := gin.Default()

	usageFlow := middlewares.UsageFlowAPI{}
	usageFlow.Init("your-api-key")

	routes := []middlewares.Route{
		{Method: "*", URL: "*"},
	}

	app.Use(usageFlow.RequestInterceptor(routes))

	app.POST("/api/v1/ledgers/measure/use", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "Handled by application"})
	})

	app.Run(":8080")
}
