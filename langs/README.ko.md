# <img src="../docs/logo_4.svg" alt="Riot" width="48" height="48"> Riot

[![PkgGoDev](https://pkg.go.dev/badge/github.com/vcaesar/riot)](https://pkg.go.dev/github.com/vcaesar/riot)
[![Tests](https://github.com/vcaesar/riot/actions/workflows/tests.yml/badge.svg?branch=main&event=push)](https://github.com/vcaesar/riot/actions/workflows/tests.yml?query=event%3Apush+branch%3Amain)
[![Lint](https://github.com/vcaesar/riot/actions/workflows/lint.yml/badge.svg?branch=main&event=push)](https://github.com/vcaesar/riot/actions/workflows/lint.yml?query=event%3Apush+branch%3Amain)
[![GitHub release](https://img.shields.io/github/release/vcaesar/riot.svg)](https://github.com/vcaesar/riot/releases/latest)
<a href="https://discord.gg/535uZPakJw"><img src="https://img.shields.io/discord/908440023093903432.svg?logo=discord&logoColor=white&label=Discord&color=5865F2" alt="Discord 채팅에 참여하세요: https://discord.gg/535uZPakJw"></a>

[English](../README.md) | [简体中文](README.zh.md) | [繁體中文](README.zht.md) | [日本語](README.ja.md) | 한국어 | [Français](README.fr.md) | [Deutsch](README.de.md) | [Español](README.es.md) | [Русский](README.ru.md) | [Português](README.pt.md)

Go로 작성된 빠르고 현대적인 전문 색인 라이브러리. [bluge](https://github.com/blugelabs/bluge)에서 포크했습니다.

riot 기반의 분산 검색 엔진이자 완전한 Elasticsearch의 경량 대안은 [zedsearch](https://github.com/vcaesar/zedsearch)를 참고하세요.

## 기능

- 지원하는 필드 타입:
  - Text, Numeric, Date, Boolean, IP, Geo Point, Vector
- 지원하는 쿼리 타입:
  - Term, Phrase, Match, Match Phrase, Prefix, Regexp, Wildcard, Fuzzy
  - Conjunction, Disjunction, Boolean
  - Numeric Range, Date Range, Term Range, IP Range
  - Geo Bounding Box, Geo Distance, Geo Polygon, ANN, KNN
- 교체 가능한 인터페이스 기반의 BM25 유사도/스코어링
- int8 양자화를 지원하는 HNSW 근사 벡터 검색; 그래프는 세그먼트와 함께 영속화되고 디스크에서 메모리 매핑됩니다
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

기본 색인 생성, 검색 및 CJK 프로그램의 실행 가능한 버전은
[`test/readme_demo`](../test/readme_demo)에 있습니다. 구조체 색인 생성과 벡터 검색 예제는
[`examples`](../examples)에 있습니다. 이 예제들의 명령은 저장소 루트에서 실행하세요.

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
쿼리 문자열 검색과 하이라이팅도 제공합니다. `cjk/main.go`로 저장한 뒤 `go run ./cjk`로 실행합니다:

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
		req := gse.QueryString(query, true)
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

출력 예시(실행 시간은 달라질 수 있습니다):

```
query "運命の犠牲者": 2 hits in 14.5µs
  1 (1.899) [見解では、謙虚なヴォードヴィリアンのベテランは、<mark>運命</mark><mark>の</mark><mark>犠</mark><mark>牲</mark><mark>者</mark>と悪役<mark>の</mark>両方<mark>の</mark>変遷として代償を払っています]
  3 (1.837) [見解では、謙虚なヴォードヴィリアンのベテランは、<mark>運命</mark><mark>の</mark><mark>犠</mark><mark>牲</mark><mark>者</mark>と悪役<mark>の</mark>両方<mark>の</mark>変遷として代償を払っています浮き沈み]
query "搜索引擎": 1 hits in 19.792µs
  5 (1.531) [Riot 是用 Go 语言编写的全文<mark>搜索</mark><mark>引擎</mark>]
query "vaudevillian": 1 hits in 1.958µs
  4 (0.654) [In view, humble <mark>vaudevillian</mark> veteran cast vicariously as both victim and villain vicissitudes of fate.]
```

### gse를 이용한 구조체 색인 생성

```go
package main

import (
	"errors"
	"fmt"
	"log"

	"github.com/vcaesar/riot/gse"
)

type Article struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() (err error) {
	index, err := gse.New(gse.Option{Lang: "en"}) // Empty Index uses memory only.
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, index.Close()) }()

	if err := index.Index("article-1", Article{
		Title: "Getting started",
		Text:  "Riot is a search engine written in Go.",
	}); err != nil {
		return err
	}

	request := gse.QueryString("started", true)
	request.Field = "title"
	result, err := index.Search(request)
	if err != nil {
		return err
	}
	for _, hit := range result.Hits {
		fmt.Printf("%s: %s %v\n", hit.ID, hit.Fields["title"], hit.Fragments["title"])
	}
	return nil
}
```

출력:

```text
article-1: Getting started [Getting <mark>started</mark>]
```

### 벡터 검색: 정확한 KNN과 근사 ANN

[`examples/knn`](../examples/knn)을 `go run ./examples/knn`으로 실행하세요:

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	riot "github.com/vcaesar/riot"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() (err error) {
	writer, err := riot.OpenWriter(riot.InMemoryOnlyConfig())
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, writer.Close()) }()

	for _, item := range []struct {
		id     string
		vector []float32
	}{
		{"east", []float32{1, 0}},
		{"northeast", []float32{1, 1}},
		{"north", []float32{0, 1}},
	} {
		field, err := riot.NewVectorField("embedding", item.vector)
		if err != nil {
			return err
		}
		doc := riot.NewDocument(item.id).AddField(field)
		if err := writer.Update(doc.ID(), doc); err != nil {
			return err
		}
	}

	reader, err := writer.Reader()
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, reader.Close()) }()

	for _, mode := range []string{"KNN", "ANN"} {
		query := riot.NewKNNQuery("embedding", []float32{1, 0}, 2).
			SetMetric(riot.Cosine)
		if mode == "ANN" {
			query.SetANN(riot.ANNParams{EfSearch: 100})
		}
		matches, err := reader.Search(context.Background(), riot.NewTopNSearch(2, query))
		if err != nil {
			return err
		}
		for {
			match, err := matches.Next()
			if err != nil {
				return err
			}
			if match == nil {
				break
			}
			var id string
			if err := match.VisitStoredFields(func(name string, value []byte) bool {
				if name == "_id" {
					id = string(value)
				}
				return true
			}); err != nil {
				return err
			}
			fmt.Printf("%s: %s (%.3f)\n", mode, id, match.Score)
		}
	}
	return nil
}
```

출력:

```text
KNN: east (1.000)
KNN: northeast (0.707)
ANN: east (1.000)
ANN: northeast (0.707)
```

### 영속화된 벡터 인덱스

HNSW 그래프는 디스크 세그먼트와 함께 저장되므로, 다시 연 인덱스는 재구축 없이 검색합니다. `WithVectorStorage`는 벡터를 둘 위치를 선택합니다: `riot.VectorStorageDisk`(기본값)는 그래프를 벡터와 함께 메모리 매핑하여 벡터가 Go 힙이 아닌 OS 페이지 캐시에 머뭅니다. `riot.VectorStorageMemory`는 링크만 저장하고 그래프를 열 때 벡터를 힙으로 읽어 들입니다(파일은 작고 메모리는 더 사용).

```go
config := riot.DefaultConfig("./riot_vectors").WithVectorStorage(riot.VectorStorageDisk)
```

[`examples/ann_disk`](../examples/ann_disk)는 디스크에 벡터를 인덱싱하고 다시 연 뒤 검색합니다: `go run ./examples/ann_disk`.

## 벤치마크

### Riot vs Bleve, Bluge (포크 원본 엔진)

세 엔진을 같은 워크로드로 측정했습니다. 합성 문서 100,000개(위치 정보와 함께 저장되는 `body` 텍스트, 숫자 `id`, `category` 키워드)를 사용했고, Bleve v2.6.1(scorch, BM25)와 Bluge v0.2.2를 기본 스트림 색인 모드의 현재 riot(Go 1.27.3)과 비교했습니다. 한 번에 프로세스 하나씩, 단일 코어(`GOMAXPROCS=1`)에서 5라운드 순환의 중앙값(Apple M1, 부하 평균 3.1~5.4)입니다. 모든 실행에서 세 엔진 모두 문서 단위 오라클과 같은 매치 수와 매치 집합을 반환했습니다.

| 워크로드 | Bleve v2.6.1 | Bluge v0.2.2 | Riot | Riot vs Bleve | Riot vs Bluge |
| --- | --- | --- | --- | --- | --- |
| 초기 색인, 문서 100,000개 | 6,546 ms | 5,029 ms | 516 ms | 12.7배 빠름 | 9.8배 빠름 |
| 문서 1개 커밋 + 가시 reader | 0.151 ms | 0.313 ms | 0.199 ms§ | 1.3배 느림 | 1.6배 빠름 |
| 디스크상 인덱스 크기 | 76.71 MB | 61.06 MB | 8.22 MB | 9.3배 작음 | 7.4배 작음 |
| 최대 메모리(RSS) | 1,931 MiB | 1,974 MiB | 144 MiB | 13.4배 적음 | 13.7배 적음 |
| 총 CPU, 전체 실행 | 17.84 s | 19.50 s | 1.08 s | 16.5배 적음 | 18.1배 적음 |
| 희귀 단어 | 11.90 µs | 16.98 µs | 2.21 µs | 5.4배 빠름 | 7.7배 빠름 |
| 흔한 단어, 상위 10개 + 개수 | 5,848 µs | 10,861 µs | 7.30 µs† | 801배 빠름 | 1,488배 빠름 |
| Boolean AND | 1,788 µs | 1,990 µs | 25.38 µs† | 70배 빠름 | 78배 빠름 |
| Boolean OR | 27.18 µs | 43.61 µs | 6.91 µs | 3.9배 빠름 | 6.3배 빠름 |
| 구문 | 60,645 µs | 61,758 µs | 2,262 µs | 27배 빠름 | 27배 빠름 |
| 접두사 `term12*`(riot에서는 상수 점수) | 3,133 µs | 3,597 µs | 50.18 µs | 62배 빠름 | 72배 빠름 |
| 퍼지 `serch`, 거리 1 | 6,798 µs | 12,035 µs | 29.98 µs† | 227배 빠름 | 401배 빠름 |
| 타입 지정 숫자 범위 | 166.2 µs | 164.6 µs | 10.56 µs | 16배 빠름 | 16배 빠름 |
| 키워드 / 패싯 term | 385.1 µs | 645.7 µs | 2.98 µs | 129배 빠름 | 217배 빠름 |
| 숫자 통계‡ | 45,375 µs | 105,356 µs | 433.6 µs | 105배 빠름 | 243배 빠름 |

- 최대 메모리와 CPU는 벤치마크 프로세스 전체(10K와 100K 빌드 및 모든 쿼리 루프)를 포함하며 `wait4` rusage에서 얻었습니다. 인덱스 크기는 별도의 디스크 빌드(3회 중앙값)에서 측정했습니다.

Bluge와 점수가 다른 이유는 riot이 Bluge의 BM25 idf를 Lucene의 `log(1 + (N − n + 0.5)/(n + 0.5))`로 수정했기 때문입니다. Bluge는 벡터 검색을 지원하지 않으며, Bleve의 벡터 검색은 `vectors` 빌드 태그와 cgo를 통한 FAISS가 필요합니다. 합성 데이터에서 단일 머신으로 측정한 수치이며 공식 벤치마크가 아닙니다.

riot(Go)는 이미 Tantivy와 대등합니다. 같은 실행에서 10개 쿼리 워크로드 중 7개에서 더 빨랐습니다. 더 빠른 riot의 Zig 버전도 개발 중입니다. 아직 beta라서 공개하지 않았습니다. 같은 벤치마크 실행에서 대부분의 쿼리에서 riot(Go)보다 약 2–5배, 색인은 2.9배 빨랐고, 모든 쿼리 워크로드에서 Tantivy보다 최소 1.5배 빨랐습니다(대부분 2–8배).

## 라이선스

Apache License Version 2.0
