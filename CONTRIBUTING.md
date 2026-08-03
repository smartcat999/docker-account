# Contributing

Issues and pull requests are welcome.

## Development

Requirements: Go 1.25+ and a Docker CLI for manual integration testing.

```bash
go test ./...
go vet ./...
go build ./...
sh -n install.sh
```

Keep changes focused and include tests for behavior changes. Never include passwords, access tokens, Keychain contents, or an unredacted Docker configuration in issues, tests, or commits.

For security issues, follow [SECURITY.md](SECURITY.md) instead of opening a public issue.
