# UsageFlow Go Middleware

[![Go Reference](https://pkg.go.dev/badge/github.com/usageflow/usageflow-go-middleware/v2.svg)](https://pkg.go.dev/github.com/usageflow/usageflow-go-middleware/v2)

The recommended UsageFlow agent for Gin applications. It meters selected HTTP
routes, applies UsageFlow policies, and sends request traces to the
[UsageFlow Console](https://console.usageflow.io).

## Prerequisites

- Go 1.23.1 or newer
- A Gin application
- A UsageFlow application API key

## Install

```bash
go get github.com/usageflow/usageflow-go-middleware/v2
export USAGEFLOW_API_KEY=uf_live_replace_me
```

## Add the middleware

```go
package main

import (
	"log"
	"os"

	"github.com/gin-gonic/gin"
	ufmiddleware "github.com/usageflow/usageflow-go-middleware/v2/pkg/middleware"
)

func main() {
	apiKey := os.Getenv("USAGEFLOW_API_KEY")
	if apiKey == "" {
		log.Fatal("USAGEFLOW_API_KEY is required")
	}

	router := gin.Default()
	usageflow := ufmiddleware.New(apiKey)
	router.Use(usageflow.RequestInterceptor())

	router.GET("/api/users/:id", func(c *gin.Context) {
		c.JSON(200, gin.H{"id": c.Param("id")})
	})

	log.Fatal(router.Run(":8080"))
}
```

Create one middleware instance during application startup and register it before
your routes. `New` opens and maintains the UsageFlow connection and starts
configuration refreshes. The package currently has no public shutdown method.

## Choose routes in the Console

In the [UsageFlow Console](https://console.usageflow.io), open the application
that owns your API key and configure:

- `monitoringPaths`: routes to meter and trace
- `whitelistEndpoints`: routes to bypass, such as health checks

The agent reads this configuration at startup and refreshes it every **30
seconds**. An empty `monitoringPaths` list means monitor every non-whitelisted
route.

Matching uses Gin route patterns such as `/api/users/:id`. A `*` matches the
entire method or entire URL field only:

- `{"method":"*","url":"/health"}` matches `/health` for every method.
- `{"method":"GET","url":"*"}` matches every GET route.
- `{"method":"*","url":"*"}` matches every route.
- `/api/*` is treated literally; it is not a prefix glob.

Whitelist matching happens before monitoring.

## Verify the integration

Start the app, then make a request:

```bash
curl -i http://localhost:8080/api/users/123
```

Open the application's Traces view in the Console and verify a trace appears for
`GET /api/users/:id`. Allow up to 30 seconds after changing route configuration.

## Optional function visibility

HTTP route tracing works with the middleware above. To also show eligible
application functions in a trace, install the CLI and build with it:

```bash
go install github.com/usageflow/usageflow-go-middleware/v2/cmd/usageflow@latest
usageflow go build ./...
```

Pass `context.Context` from the Gin request into service functions. Function
visibility currently covers functions in your module whose first parameter is
`context.Context` or `*gin.Context`.

## Runtime behavior

- A disconnected UsageFlow service does not stop your handler; metering is
  skipped until connectivity returns.
- A configured blocked-endpoint policy returns HTTP `403` with
  `error: "endpoint_blocked"`.
- A rate-limit or quota denial returns HTTP `429` with
  `error: "rate_limit_exceeded"`.
- An unexpected connected allocation failure returns HTTP `400` with
  `error: "Request allocation failed"`.
- The agent captures method, Gin route pattern, raw path, client IP, user agent,
  query and path parameters, request body, response status, duration, and
  response body.
- `Authorization` values and headers named `x-...key` are masked. Other headers
  are included. Response capture is limited to 512 KiB; long non-JSON response
  text is summarized rather than retained.

Review the captured data and configure routes narrowly for production. Do not
put API keys in source control.

## Troubleshooting

- **No trace:** confirm `USAGEFLOW_API_KEY`, application selection, and that the
  Gin route pattern is monitored and not whitelisted.
- **Config change not visible:** wait at least 30 seconds and send a new request.
- **Dynamic route mismatch:** configure Gin's pattern (`/users/:id`), not a
  concrete path (`/users/123`).
- **App works but UsageFlow is absent:** check outbound WebSocket access to
  `wss://api.usageflow.io/ws`.
- **Functions absent:** build with `usageflow go build` and propagate the request
  context into eligible functions.

## Migrating from the legacy package

If you use `github.com/usageflow/usageflow-gin`, switch the module import to
`github.com/usageflow/usageflow-go-middleware/v2`, change
`RequestInterceptor(routes, whitelist)` to `RequestInterceptor()`, and move
route configuration to the Console. The legacy repository is currently
unavailable remotely, so no link is included here.

- Repository: [github.com/usageflow/usageflow-go-middleware](https://github.com/usageflow/usageflow-go-middleware)
- Package reference: [pkg.go.dev/github.com/usageflow/usageflow-go-middleware/v2](https://pkg.go.dev/github.com/usageflow/usageflow-go-middleware/v2)

Before releasing documentation changes, complete [RELEASE_CHECKLIST.md](RELEASE_CHECKLIST.md).

MIT — see [LICENSE](LICENSE).
