# AGENTS.md — riot

Go text indexing/search library (`github.com/vcaesar/riot`, Bluge fork, Apache-2.0).
Root package is `riot`; `cmd/riot` is a small cobra CLI. Go version from `go.mod`.

## Commands

```bash
go build ./...
go test -race ./...                      # CI gate (.github/workflows/tests.yml, linux/macOS/windows)
go test -run TestCrud .                  # single test
go test ./test/ -segType ice -segVer 1   # integration suite against a specific segment plugin
golangci-lint run                        # CI uses golangci-lint-action v9
gofmt -l . && goimports -w <file>
```

No Makefile, codegen, vendor dir, or fixture downloads; test corpora are inline (`test/fosdem_test.go`).

## Architecture

- Root `riot` — public API: `config.go` (`DefaultConfig`, `InMemoryOnlyConfig`,
  `DefaultConfigWithDirectory`), `writer.go`, `reader.go`, `document.go`, `field.go`,
  `query.go`, `search.go`, `multisearch.go`, `batch.go`. `Config` wraps `index.Config` and
  injects the `_all` field and similarity-derived norm calculator.
- `index/` — storage engine: `writer.go`, `persister.go`, `introducer.go`, `merge.go`,
  `snapshot.go`, `directory*.go`, `lock/`, `mergeplan/`. Segment formats plug in via
  `index.SegmentPlugin` (`index/segment_plugin.go:26`); only ice v1 is registered and default
  (`index/config.go:168`).
- `search/` — `searcher/`, `collector/`, `similarity/` (BM25), `aggregations/`, `highlight/`.
- `analysis/` — `char/`, `tokenizer/`, `token/`, `analyzer/`, `lang/<code>/`.
- `numeric/` — numeric/prefix encoding, `numeric/geo/`.
- `test/` — integration tests driven by `IntegrationTest`/`RequestVerify` tables (`test/integration.go`).

## Code style

- Apache-2.0 header on every `.go` file; new files use "The Riot Authors".
- Fluent value-receiver builders return copies: `func (config Config) WithSegmentType(typ string) Config`.
- `.golangci.yml`: `lll` 140, `funlen`, `gocyclo` 20, `dupl` 100, `misspell` US.
  `gochecknoinits` is on — `init()` only in `_test.go`, `sizes.go`, `cmd/riot/cmd`.
- Errors: `fmt.Errorf("error <doing x>: %v", err)`.

## Testing

- Same-package tests, table-driven with `t.Run`, `t.Fatalf`/`t.Errorf`, no assert lib.
- Temp indexes: `createTmpIndexPath(t)` + `defer cleanupTmpIndexPath(t, path)` (`index_test.go:42`),
  or `InMemoryOnlyConfig()`.
- `_test.go` is exempt from `lll`, `funlen`, `dupl`, `gocyclo`, `goconst`, `gosec` G404.

## Pitfalls

- Default segment type/version in `index/config.go` defines on-disk format; do not change casually.
- `Close()` writers and every `writer.Reader()`; segments are refcounted, leaked refs keep files open.
- Persist/merge run asynchronously (`persister.go`, `introducer.go`, `merge.go`); assert on
  reader snapshots, never on file counts or timing.
- `index/directory_fs_nix.go` (`//go:build` tag) and `directory_fs_windows.go` (filename
  constraint) must change together.
- `go test -race ./...` is the gate; index concurrency bugs only surface there.
