# ![Riot](../docs/bluge.png) Riot

[![PkgGoDev](https://pkg.go.dev/badge/github.com/vcaesar/riot)](https://pkg.go.dev/github.com/vcaesar/riot)
[![Tests](https://github.com/vcaesar/riot/actions/workflows/tests.yml/badge.svg?branch=main&event=push)](https://github.com/vcaesar/riot/actions/workflows/tests.yml?query=event%3Apush+branch%3Amain)
[![Lint](https://github.com/vcaesar/riot/actions/workflows/lint.yml/badge.svg?branch=main&event=push)](https://github.com/vcaesar/riot/actions/workflows/lint.yml?query=event%3Apush+branch%3Amain)

[English](../README.md) | 简体中文 | [繁體中文](README.zht.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Français](README.fr.md) | [Deutsch](README.de.md) | [Español](README.es.md) | [Русский](README.ru.md) | [Português](README.pt.md)

快速、现代的 Go 语言全文索引库，fork 自 [bluge](https://github.com/blugelabs/bluge)

## 特性

- 支持的字段类型：
  - 文本（Text）、数值（Numeric）、日期（Date）、布尔（Boolean）、IP、地理坐标（Geo Point）、向量（Vector）
- 支持的查询类型：
  - Term、Phrase、Match、Match Phrase、Prefix、Regexp、Wildcard、Fuzzy
  - Conjunction、Disjunction、Boolean
  - 数值范围（Numeric Range）、日期范围（Date Range）、词项范围（Term Range）、IP 范围（IP Range）
  - 地理矩形（Geo Bounding Box）、地理距离（Geo Distance）、地理多边形（Geo Polygon）、KNN
- BM25 相似度/打分，接口可插拔
- 搜索结果匹配高亮
- 可扩展的聚合：
  - 分桶（Bucketing）
    - Terms
    - 数值范围
    - 日期范围
  - 指标（Metrics）
    - Min/Max/Count/Sum
    - Avg/Weighted Avg
    - 基数估计（[HyperLogLog++](https://github.com/axiomhq/hyperloglog)）
    - 分位数近似（[T-Digest](https://github.com/caio/go-tdigest)）

## 安装

```sh
go get -u github.com/vcaesar/riot
```

## 使用

下面三个程序的可运行版本位于 [`test/readme_demo`](../test/readme_demo)。

### 建立索引

保存为 `write/main.go`，然后运行 `go run ./write`：

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

### 查询

保存为 `read/main.go`，然后针对上面写入的索引运行 `go run ./read`。
如果在同一进程中既建索引又搜索，请使用 `writer.Reader()` 而不是 `riot.OpenReader`
（或者使用 `riot.InMemoryOnlyConfig()` 创建内存索引）：

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

输出：

```
match: example
```

### 使用 gse 处理中文 / 日文

[`gse`](../gse) 包用 [gse](https://github.com/go-ego/gse) 分词器封装了 riot，
用于 CJK 文本，并提供查询字符串搜索和高亮。保存为 `gse/main.go`，然后运行 `go run ./gse`：

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

输出：

```
query "運命の犠牲者": 2 hits in 14.5µs
  1 (1.909) [見解では、謙虚なヴォードヴィリアンのベテランは、<mark>運命</mark><mark>の</mark><mark>犠</mark><mark>牲</mark><mark>者</mark>と悪役<mark>の</mark>両方<mark>の</mark>変遷として代償を払っています]
  3 (1.846) [見解では、謙虚なヴォードヴィリアンのベテランは、<mark>運命</mark><mark>の</mark><mark>犠</mark><mark>牲</mark><mark>者</mark>と悪役<mark>の</mark>両方<mark>の</mark>変遷として代償を払っています浮き沈み]
query "搜索引擎": 1 hits in 19.792µs
  5 (1.536) [Riot 是用 Go 语言编写的全文<mark>搜索</mark><mark>引擎</mark>]
query "vaudevillian": 1 hits in 1.958µs
  4 (0.657) [In view, humble <mark>vaudevillian</mark> veteran cast vicariously as both victim and villain vicissitudes of fate.]
```

## 许可证

Apache License Version 2.0
