# Release checklist

Complete this checklist before publishing a release or documentation update:

- [ ] README examples compile against the exported API.
- [ ] `New(apiKey)` and `RequestInterceptor()` signatures match the current package.
- [ ] Go version and installation commands match `go.mod`.
- [ ] Repository, pkg.go.dev, Console, migration, license, and local document links work.
- [ ] Quickstart succeeds from a clean Gin application using `USAGEFLOW_API_KEY`.
- [ ] Console route matching, refresh timing, policy responses, captured data, and fail-soft behavior match tests and code.
- [ ] `go test ./...` passes.
