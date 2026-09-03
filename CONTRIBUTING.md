# Contributing

Contributions are welcome. Keep adapters conservative: preserve unrelated configuration, never log credentials, and fail rather than guess an endpoint or overwrite an ambiguous custom setup.

Before submitting a change:

```bash
gofmt -w cmd internal
go test ./...
go test -race ./...
go vet ./...
```

New adapters must include detection, planning, idempotence, verification, rollback coverage, malformed-input tests, and documentation of their configuration precedence rules.
