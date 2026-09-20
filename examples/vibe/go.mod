module vibe-example

go 1.23.1

require github.com/usageflow/usageflow-go-middleware/v2 v2.0.0-00010101000000-000000000000

require (
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
)

replace github.com/usageflow/usageflow-go-middleware/v2 => ../..
