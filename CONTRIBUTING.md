# Contributing

Contributions are welcome. Keep adapters conservative: preserve unrelated configuration, never log credentials, and fail rather than guess an endpoint or overwrite an ambiguous custom setup.

Before submitting a change:

```bash
gofmt -w cmd internal
go test ./...
go test -race ./...
go vet ./...
```

New adapters must include detection, planning, idempotence, protocol-aware `ProbeTargets`, verification, rollback coverage, malformed-input tests, and documentation of their configuration precedence rules. Mirror ranking belongs in the shared benchmark engine; adapters must not invent their own strategy or call one latency sample an absolute fastest mirror.
