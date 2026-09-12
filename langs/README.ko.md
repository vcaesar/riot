# ![Riot](../docs/bluge.png) Riot

[![PkgGoDev](https://pkg.go.dev/badge/github.com/vcaesar/riot)](https://pkg.go.dev/github.com/vcaesar/riot)
[![Tests](https://github.com/vcaesar/riot/actions/workflows/tests.yml/badge.svg?branch=main&event=push)](https://github.com/vcaesar/riot/actions/workflows/tests.yml?query=event%3Apush+branch%3Amain)
[![Lint](https://github.com/vcaesar/riot/actions/workflows/lint.yml/badge.svg?branch=main&event=push)](https://github.com/vcaesar/riot/actions/workflows/lint.yml?query=event%3Apush+branch%3Amain)

[English](../README.md) | [简体中文](README.zh.md) | [繁體中文](README.zht.md) | [日本語](README.ja.md) | 한국어 | [Français](README.fr.md) | [Deutsch](README.de.md) | [Español](README.es.md) | [Русский](README.ru.md) | [Português](README.pt.md)

Go로 작성된 빠르고 현대적인 전문 색인 라이브러리. [bluge](https://github.com/blugelabs/bluge)에서 포크했습니다.

## 기능

- 지원하는 필드 타입:
  - Text, Numeric, Date, Boolean, IP, Geo Point, Vector
- 지원하는 쿼리 타입:
  - Term, Phrase, Match, Match Phrase, Prefix, Regexp, Wildcard, Fuzzy
  - Conjunction, Disjunction, Boolean
  - Numeric Range, Date Range, Term Range, IP Range
  - Geo Bounding Box, Geo Distance, Geo Polygon, KNN
- 교체 가능한 인터페이스 기반의 BM25 유사도/스코어링
- 검색 결과 매치 하이라이팅
- 확장 가능한 집계:
  - 버킷팅
    - Terms
    - Numeric Range
    - Date Range
  - 메트릭
    - Min/Max/Count/Sum
    - Avg/Weighted Avg
    - 카디널리티 추정 ([HyperLogLog++](https://github.com/axiomhq/hyperloglog))
    - 분위수 근사 ([T-Digest](https://github.com/caio/go-tdigest))

## 설치

```sh
go get -u github.com/vcaesar/riot
```

## 사용법

아래 세 프로그램의 실행 가능한 버전은 [`test/readme_demo`](../test/readme_demo)에 있습니다.

### 색인 생성

`write/main.go`로 저장한 뒤 `go run ./write`로 실행합니다:

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

### 검색

`read/main.go`로 저장한 뒤 위에서 만든 색인을 대상으로 `go run ./read`로 실행합니다.
같은 프로세스에서 색인과 검색을 함께 수행한다면 `riot.OpenReader` 대신 `writer.Reader()`를 사용하세요
(인메모리 색인은 `riot.InMemoryOnlyConfig()`를 사용합니다):

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

출력:

```
match: example
```

### gse를 이용한 중국어 / 일본어

[`gse`](../gse) 패키지는 CJK 텍스트를 위해 [gse](https://github.com/go-ego/gse) 토크나이저로 riot을 감싸며,
쿼리 문자열 검색과 하이라이팅도 제공합니다. `gse/main.go`로 저장한 뒤 `go run ./gse`로 실행합니다:

```go
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/vcaesar/riot/gse"
)

func main() {
	opt := gse.Option{
		Index: "test.riot",
		Dicts: "embed, ja", // or "embed, zh"
		Opt:   "search-hmm",
	}
	// Option{Lang: "en"} skips gse and uses a riot analysis/lang analyzer
	// instead ("en", "cjk", "de", ...; see gse.Langs()).
	defer os.RemoveAll(opt.Index)

	index, err := gse.New(opt)
	if err != nil {
		log.Fatalf("error opening gse index: %v", err)
	}
	defer index.Close()

	text := `見解では、謙虚なヴォードヴィリアンのベテランは、運命の犠牲者と悪役の両方の変遷として代償を払っています`
	docs := map[string]string{
		"1": text,
		"3": text + "浮き沈み",
		"4": `In view, a humble vaudevillian veteran cast vicariously as both victim and villain vicissitudes of fate.`,
		"2": `It's difficult to understand the sum of a person's life.`,
		"5": `Riot 是用 Go 语言编写的全文搜索引擎`,
	}
	for id, doc := range docs {
		if err = index.Index(id, doc); err != nil {
			log.Fatalf("error indexing %s: %v", id, err)
		}
	}

	for _, query := range []string{"運命の犠牲者", "搜索引擎", "vaudevillian"} {
		req := gse.NewQueryString(query, true)
		res, err := index.Search(req)
		if err != nil {
			log.Fatalf("error searching %q: %v", query, err)
		}
		fmt.Printf("query %q: %d hits in %v\n", query, res.Total, res.Took)
		for _, hit := range res.Hits {
			fmt.Printf("  %s (%.3f) %v\n", hit.ID, hit.Score, hit.Fragments["text"])
		}
	}
}
```

출력:

```
query "運命の犠牲者": 2 hits in 14.5µs
  1 (1.909) [見解では、謙虚なヴォードヴィリアンのベテランは、<mark>運命</mark><mark>の</mark><mark>犠</mark><mark>牲</mark><mark>者</mark>と悪役<mark>の</mark>両方<mark>の</mark>変遷として代償を払っています]
  3 (1.846) [見解では、謙虚なヴォードヴィリアンのベテランは、<mark>運命</mark><mark>の</mark><mark>犠</mark><mark>牲</mark><mark>者</mark>と悪役<mark>の</mark>両方<mark>の</mark>変遷として代償を払っています浮き沈み]
query "搜索引擎": 1 hits in 19.792µs
  5 (1.536) [Riot 是用 Go 语言编写的全文<mark>搜索</mark><mark>引擎</mark>]
query "vaudevillian": 1 hits in 1.958µs
  4 (0.657) [In view, humble <mark>vaudevillian</mark> veteran cast vicariously as both victim and villain vicissitudes of fate.]
```

## 라이선스

Apache License Version 2.0
