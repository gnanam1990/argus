# Contributing to Argus

Thanks for your interest. Argus is built in disciplined, reviewable stages.

## Ground rules

- **Discuss first.** Open (or claim) an issue describing the change before
  sending a substantial PR, so scope and approach are agreed up front.
- **Stay in scope.** Each PR should do one coherent thing. Prefer a small number
  of well-batched PRs over a scatter of tiny ones.
- **Fakes before reals.** Every interface seam (`Computer`, `Provider`,
  `Grounder`, `Sandbox`) ships with a test fake. New backends must not require a
  real display, network, or container for the default `go test ./...`.
- **Tests are not optional.** New behavior comes with table-driven tests.
  Integration tests that need a display/network/Docker are gated behind build
  tags so the default test run stays hermetic.

## Before you push

```sh
make lint    # go vet + staticcheck, must be clean
make test    # go test -race ./..., must pass
gofmt -l .   # must print nothing
```

## Secrets

Never commit API keys, tokens, or `.env` files — not even for testing. Argus
reads all secrets from the environment. CI fails the build if a secret-like file
or key pattern is committed. If a secret is ever leaked, **rotate it
immediately** and report it; do not try to "fix forward" by deleting the file in
a later commit.

## Commit messages

Use clear, imperative, Conventional-Commits-style messages
(`feat:`, `fix:`, `test:`, `docs:`, `chore:`). Keep messages focused on what and
why. Every commit should build and pass tests on its own where practical.
