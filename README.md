# ![Riot](docs/bluge.png) Riot

[![PkgGoDev](https://pkg.go.dev/badge/github.com/vcaesar/riot)](https://pkg.go.dev/github.com/vcaesar/riot)
[![Tests](https://github.com/vcaesar/riot/actions/workflows/tests.yml/badge.svg?branch=main&event=push)](https://github.com/vcaesar/riot/actions/workflows/tests.yml?query=event%3Apush+branch%3Amain)
[![Lint](https://github.com/vcaesar/riot/actions/workflows/lint.yml/badge.svg?branch=main&event=push)](https://github.com/vcaesar/riot/actions/workflows/lint.yml?query=event%3Apush+branch%3Amain)

The fast modern text indexing in go, fork form the [bluge](https://github.com/blugelabs/bluge)

## Features

- Supported field types:
  - Text, Numeric, Date, Geo Point
- Supported query types:
  - Term, Phrase, Match, Match Phrase, Prefix
  - Conjunction, Disjunction, Boolean
  - Numeric Range, Date Range
- BM25 Similarity/Scoring with pluggable interfaces
- Search result match highlighting
- Extendable Aggregations:
  - Bucketing
    - Terms
    - Numeric Range
    - Date Range
  - Metrics
    - Min/Max/Count/Sum
    - Avg/Weighted Avg
    - Cardinality Estimation ([HyperLogLog++](https://github.com/axiomhq/hyperloglog))
    - Quantile Approximation ([T-Digest](https://github.com/caio/go-tdigest))

## Installation

```sh
go get -u github.com/vcaesar/riot
```

## Usage

Runnable versions of both programs live in [`test/readme_demo`](test/readme_demo).

### Indexing

Save as `write/main.go` and run with `go run ./write`:

```go
package main

import (
	"log"

	riot "github.com/vcaesar/riot"
)

func main() {
	config := riot.DefaultConfig("./riot_index")
	writer, err := riot.OpenWriter(config)
	if err != nil {
		log.Fatalf("error opening writer: %v", err)
	}
	defer writer.Close()

	doc := riot.NewDocument("example").
		AddField(riot.NewTextField("name", "riot"))

	err = writer.Update(doc.ID(), doc)
	if err != nil {
		log.Fatalf("error updating document: %v", err)
	}
	log.Printf("indexed: %s", doc.ID())
}
```

### Querying

Save as `read/main.go` and run with `go run ./read` against the index written above.
If you index and search in the same process, use `writer.Reader()` instead of `riot.OpenReader`
(or `riot.InMemoryOnlyConfig()` for an in-memory index):

```go
package main

import (
	"context"
	"fmt"
	"log"

	riot "github.com/vcaesar/riot"
)

func main() {
	config := riot.DefaultConfig("./riot_index")
	reader, err := riot.OpenReader(config)
	if err != nil {
		log.Fatalf("error opening reader: %v", err)
	}
	defer reader.Close()

	query := riot.NewMatchQuery("riot").SetField("name")
	request := riot.NewTopNSearch(10, query).
		WithStandardAggregations()
	documentMatchIterator, err := reader.Search(context.Background(), request)
	if err != nil {
		log.Fatalf("error executing search: %v", err)
	}

	match, err := documentMatchIterator.Next()
	for err == nil && match != nil {
		err = match.VisitStoredFields(func(field string, value []byte) bool {
			if field == "_id" {
				fmt.Printf("match: %s\n", string(value))
			}
			return true
		})
		if err != nil {
			log.Fatalf("error loading stored fields: %v", err)
		}
		match, err = documentMatchIterator.Next()
	}
	if err != nil {
		log.Fatalf("error iterator document matches: %v", err)
	}
}
```

Output:

```
match: example
```

<!-- ## Repobeats

![Alt](https://repobeats.axiom.co/api/embed/0d7f8bc7927e15b07f1ae592eeff01811c5a2f80.svg "Repobeats analytics image") -->

## License

Apache License Version 2.0
