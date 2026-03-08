# arc

`arc` is a typed HTTP/API runtime core for Go.

It provides:
- a custom router
- typed handlers
- request binding and validation
- error mapping
- response encoding
- OpenAPI 3.1 + JSON Schema generation
- middleware and observability hooks
- built-in Swagger UI docs

## Scope

This README is the canonical scope/status document for the repository.

Implemented:
- custom router (path params, catch-all, groups, HEAD/OPTIONS)
- typed handlers (`Response`, raw, stream, SSE, websocket helper)
- binding from path/query/header/cookie/body/form/multipart (including multipart files)
- validation (required, min/max, min/max length, regex, enum, format, cross-field, custom validators)
- OpenAPI 3.1 and JSON Schema generation
- content negotiation (`application/json`, `application/problem+json`, vendor `+json` matching)
- middleware pipeline and observability hooks
- tenant extraction (header/path/cookie/JWT)
- query DTO + include parsing + include tree helpers
- API versioning middleware and route version markers
- response caching with ETag/304 and invalidation middleware
- OpenAPI callbacks support
- server lifecycle (`/health`, `/ready`, graceful shutdown)

`arc` currently integrates with `orm` directly (`arc -> orm`).

The adapter abstraction layer is **not implemented yet by design** in the current scope.
This is a known and intentional limitation for the current version.

## Quick Start

```go
e := arc.New()

type In struct {
    ID int64 `path:"id" validate:"required,min=1"`
}
type Out struct {
    ID int64 `json:"id"`
}

arc.Handle(e, "GET", "/users/{id}", "users_get", func(ctx context.Context, in *In) (*arc.Response[Out], error) {
    return arc.OK(Out{ID: in.ID}), nil
})

e.RegisterSystemRoutes("/openapi.json", "/docs")
```

## CLI: OpenAPI Generator

`cmd/arc` generates OpenAPI from registered routes.

Example:

```bash
go run ./cmd/arc -format json -out openapi.json
go run ./cmd/arc -format yaml -out openapi.yaml
go run ./cmd/arc -format json -stdout
```

Flags:
- `-out` output file path (default: `openapi.json`)
- `-format` `json|yaml` (default: `json`)
- `-stdout` print spec to stdout instead of writing file
- `-with-system` include system routes (`/openapi.json`, `/openapi.yaml`, `/docs`, `/schemas`)

To plug your own route registrations for generation, add a file in `cmd/arc` and assign `RegisterRoutes` in `init()`:

```go
func init() {
    RegisterRoutes = func(e *arc.Engine) {
        // register routes here
    }
}
```

## Built-in Endpoints

- `/openapi.json`
- `/openapi.yaml`
- `/docs` (Swagger UI)
- `/schemas`
- `/schemas/{name}`
- `/health`
- `/ready`

## CI/CD

GitHub Actions workflows:
- `CI` (`.github/workflows/ci.yml`): `go vet`, `go test`, and `go build ./cmd/arc` on push/PR.
- `Release` (`.github/workflows/release.yml`): builds `cmd/arc` binaries for Linux/macOS/Windows on `v*` tags and publishes assets to GitHub Releases.

## Development

Run tests:

```bash
go test ./...
```

## License

MIT. See [LICENSE.md](LICENSE.md).
