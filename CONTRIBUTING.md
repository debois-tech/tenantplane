# Contributing

tenantplane is early. The most useful contributions right now are design reviews, test cases for sync edge cases, and small implementation slices.

## Development

Run the local checks:

```sh
go test ./...
go build ./cmd/tenantplane
```

Keep the first implementation boring and inspectable. If a sync rule cannot be explained to an operator, it is not ready.

