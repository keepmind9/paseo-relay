# Contributing to Paseo Relay

Thanks for your interest in contributing! This guide covers the basics.

## Development Setup

```bash
git clone https://github.com/keepmind9/paseo-relay.git
cd paseo-relay
make build
make test
```

**Requirements:** Go 1.22+

## Making Changes

1. Fork the repository
2. Create a feature branch (`git checkout -b feat/my-feature`)
3. Make your changes
4. Run `make fmt` to format code
5. Run `make test` to verify all tests pass
6. Commit with a descriptive message (see conventions below)
7. Open a Pull Request

## Commit Message Convention

We use conventional commit prefixes:

- `feat:` new feature
- `fix:` bug fix
- `docs:` documentation
- `refactor:` code refactoring (no behavior change)
- `opt:` performance optimization
- `security:` security fixes
- `chore:` build/tooling changes

Subject lines should be under 72 characters.

## Code Style

- Follow standard Go conventions (`gofmt`, `go vet`)
- All code, comments, and commit messages must be in English
- Add doc comments to all exported types and functions
- Keep the test coverage above 50%

## Testing

- Use `github.com/stretchr/testify` for assertions
- Prefer table-driven tests for multiple scenarios
- Run the full suite before submitting: `make test`

## Reporting Issues

- Search existing issues before opening a new one
- Include steps to reproduce, expected vs actual behavior, and Go version
- For security vulnerabilities, please email the maintainer directly instead of opening a public issue

## Pull Requests

- One logical change per PR
- Keep PRs focused and reasonably sized
- Ensure CI passes (build + tests)
- New features should include tests

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
