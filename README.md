# UsageFlow Go Middleware

[![Go Reference](https://pkg.go.dev/badge/github.com/usageflow/usageflow-go-middleware.svg)](https://pkg.go.dev/github.com/usageflow/usageflow-go-middleware)
[![Go Report Card](https://goreportcard.com/badge/github.com/usageflow/usageflow-go-middleware.svg)](https://goreportcard.com/report/github.com/usageflow/usageflow-go-middleware)

> ⚠️ **Beta Notice**: This package is currently in beta for experimentation. While we strive to maintain stability, breaking changes may occur as we refine the API and features. We recommend testing thoroughly in development environments before deploying to production.

A Go middleware package for integrating UsageFlow with Gin: HTTP metering plus **automatic function call-chain instrumentation** (same `report_call_chain` wire format as the JS and Python agents).

## Installation

```bash
go get github.com/usageflow/usageflow-go-middleware/v2
go install github.com/usageflow/usageflow-go-middleware/v2/cmd/usageflow@latest
```

## Quick Start

### 1. Add Gin middleware (runtime)

```go
package main

import (
    "context"

    "github.com/gin-gonic/gin"
    ufmiddleware "github.com/usageflow/usageflow-go-middleware/v2/pkg/middleware"
)

func listUsers(ctx context.Context) ([]string, error) {
    return []string{"user1", "user2"}, nil
}

func main() {
    r := gin.Default()
    uf := ufmiddleware.New("your-api-key")
    r.Use(uf.RequestInterceptor())

    r.GET("/api/users", func(c *gin.Context) {
        users, _ := listUsers(c.Request.Context())
        c.JSON(200, gin.H{"users": users})
    })
    r.Run(":8080")
}
```

No `Track` / `Wrap` in application code. Pass `c.Request.Context()` into services (normal Go style) so functions correlate to the HTTP request.

### 2. Build with UsageFlow (compile-time)

JS/Python agents patch modules at runtime. Go is statically compiled, so instrumentation is injected at **build** time:

```bash
usageflow go build -o server .
# or
usageflow go test ./...
```

Under the hood this rewrites eligible sources via Go’s `-overlay` mechanism (so new imports participate in the module graph) and instruments functions in your module that take `context.Context` or `*gin.Context` as their first parameter.

After each request, middleware sends `report_call_chain` with `method`, `url`, `usageflowRequestId`, and the recorded functions.

### Disable discovery

- Server: `discoveryDisabled` on `get_application_config`
- Client: `USAGEFLOW_DISCOVERY_DISABLED=true`

## JS / Python vs Go

| | JS / Python | Go |
|---|---|---|
| How functions are found | Runtime import / `require` hooks | Compile-time rewrite (`usageflow go build`) |
| User wraps each function? | No | No (when using the CLI) |
| Request correlation | ALS / contextvars | `context.Context` on the request + call chain |
| Report message | `report_call_chain` | `report_call_chain` (same shape) |
| Per-call schemas | `paramsSchema` / `resultSchema` | Same fields on each call record |
| Function metering | `request_for_allocation` / `use_allocation` with `type: FUNCTION_CALL` | Same; respects `reportAllFunctionAllocations` + `FUNCTION` policies |
| Block on function | Rate-limited `FUNCTION` policy via async allocate | Same for instrumented funcs that return `error` |

Each call record can include `paramsSchema`, `resultSchema`, optional LLM `usage` / `aiModel`, and `usageflowRequestId`. Function policies from `get_application_policies` with `type: "FUNCTION"` use identity `func:<filePath>:<funcName>` (same key shape as JS). When `hasRateLimit` is true and allocation is denied, instrumented functions that return `error` abort with a policy error.

**v1 scope:** functions whose first argument is `context.Context` or `*gin.Context`, in your main module. Calls that drop context (or start goroutines without it) may not appear until later propagation work.

## Configuration

Monitor / whitelist routes are loaded from the UsageFlow server via the WebSocket (`get_application_config`). Initialize with your API key:

```go
uf := ufmiddleware.New("your-api-key")
r.Use(uf.RequestInterceptor())
```

### Production rate-limit enforcement with Gin

Register `RequestInterceptor` before registering routes. Gin must know the matched route pattern (for example, `/users/:id`), and the UsageFlow policy method and URL must exactly match that pattern.

For routes that must be rate limited, fetch and validate policies synchronously before the server starts accepting traffic. `New` also starts a background configuration updater, but background fetch errors do not stop the application:

```go
uf := ufmiddleware.New(os.Getenv("USAGEFLOW_API_KEY"))

policies, err := uf.FetchApiConfig()
if err != nil {
    log.Fatalf("UsageFlow policies unavailable: %v", err)
}

requiredMethod := http.MethodPost
requiredURL := "/api/v1/discover"
found := false
for _, policy := range policies {
    if policy.Method == requiredMethod && policy.Url == requiredURL && policy.HasRateLimit {
        found = true
        break
    }
}
if !found {
    log.Fatalf("required UsageFlow rate limit is not configured for %s %s", requiredMethod, requiredURL)
}

r.Use(uf.RequestInterceptor()) // before r.POST, r.GET, groups, and other routes
r.POST(requiredURL, discoverHandler)
```

If the policy identity comes from a proxy header, explicitly forward that header and prevent clients from reaching the Gin port directly. For Cloudflare behind Nginx:

```nginx
proxy_set_header Cf-Connecting-Ip $http_cf_connecting_ip;
proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
```

Configure Gin trusted proxies with `SetTrustedProxies`; do not trust every proxy in production. Test the deployed path—not only the handler—with more requests than the configured limit and assert that the handler is not invoked after an HTTP `429`.

Routes with `hasRateLimit: true` fail closed on **explicit quota/policy denials**. If the UsageFlow WebSocket is down or a transport timeout occurs, the middleware **fails open** so the customer API stays up; rate limiting resumes after reconnect. A local limiter is still recommended as an independent abuse fallback.

## Advanced: manual Track / Wrap

If a function cannot be auto-instrumented, you can opt in:

```go
import "github.com/usageflow/usageflow-go-middleware/v2/pkg/tracker"

result, err := tracker.Track(ctx, tracker.Options{
    FuncName: "specialCase",
    FilePath: "svc/special.go",
}, func() (T, error) { return special(ctx) })
```

Prefer `usageflow go build` for normal application code.

## Example

Run the example locally without an API key:

```bash
./examples/basic/run-local.sh
```

Then open:

```text
http://127.0.0.1:8080/api/go/instrumentation-demo
```

The JSON response includes `callChain` with the automatically instrumented
`listUsers` function. To additionally send the chain to UsageFlow, export
`USAGEFLOW_API_KEY` before running the script.

### Live chat UI

The [`examples/chat`](examples/chat) app provides a browser chat UI backed by
Gin and the live UsageFlow WebSocket:

```bash
cd examples/chat
cp .env.example .env
# Add your UsageFlow application key to .env
./run-live.sh
```

Open `http://127.0.0.1:8081`. Each chat message executes several automatically
instrumented Go functions, displays the local call chain, and sends it to
UsageFlow as `report_call_chain`.

## Release Process

This package uses GitHub Actions to create releases when changes are pushed to `main`.

## Documentation

For detailed documentation, see [docs.usageflow.io](https://docs.usageflow.io).

## Security

### Known Vulnerabilities

This package currently uses `golang.org/x/net v0.25.0` which has a known vulnerability:
- **GO-2025-3595**: Incorrect Neutralization of Input During Web Page Generation in x/net
  - **Severity**: Critical
  - **Fixed in**: `golang.org/x/net v0.38.0` (requires Go 1.24.0+)

### Go Version Requirements

- **Minimum**: Go 1.23.1
- **Recommended**: Go 1.24.0+

## Release Notes

See [RELEASE_NOTES.md](RELEASE_NOTES.md).

## License

MIT — see [LICENSE](LICENSE).
