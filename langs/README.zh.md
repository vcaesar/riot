# <img src="../docs/logo_4.svg" alt="Riot" width="48" height="48"> Riot

[![PkgGoDev](https://pkg.go.dev/badge/github.com/vcaesar/riot)](https://pkg.go.dev/github.com/vcaesar/riot)
[![Tests](https://github.com/vcaesar/riot/actions/workflows/tests.yml/badge.svg?branch=main&event=push)](https://github.com/vcaesar/riot/actions/workflows/tests.yml?query=event%3Apush+branch%3Amain)
[![Lint](https://github.com/vcaesar/riot/actions/workflows/lint.yml/badge.svg?branch=main&event=push)](https://github.com/vcaesar/riot/actions/workflows/lint.yml?query=event%3Apush+branch%3Amain)

[English](../README.md) | 简体中文 | [繁體中文](README.zht.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Français](README.fr.md) | [Deutsch](README.de.md) | [Español](README.es.md) | [Русский](README.ru.md) | [Português](README.pt.md)

快速、现代的 Go 语言全文索引库，fork 自 [bluge](https://github.com/blugelabs/bluge)

如需基于 riot 的分布式搜索引擎、完整 Elasticsearch 的轻量替代方案，请参阅 [zed](https://github.com/vcaesar/zedsearch)。

## 特性

- 支持的字段类型：
  - 文本（Text）、数值（Numeric）、日期（Date）、布尔（Boolean）、IP、地理坐标（Geo Point）、向量（Vector）
- 支持的查询类型：
  - Term、Phrase、Match、Match Phrase、Prefix、Regexp、Wildcard、Fuzzy
  - Conjunction、Disjunction、Boolean
  - 数值范围（Numeric Range）、日期范围（Date Range）、词项范围（Term Range）、IP 范围（IP Range）
  - 地理矩形（Geo Bounding Box）、地理距离（Geo Distance）、地理多边形（Geo Polygon）、ANN、KNN
- BM25 相似度/打分，接口可插拔
- HNSW 近似向量搜索，支持 int8 量化；图随段持久化，并从磁盘内存映射
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

基本索引、查询和 CJK 程序的可运行版本位于 [`test/readme_demo`](../test/readme_demo)。
结构体索引和向量搜索示例位于 [`examples`](../examples)；请在仓库根目录运行这些示例的命令。

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
用于 CJK 文本，并提供查询字符串搜索和高亮。保存为 `cjk/main.go`，然后运行 `go run ./cjk`：

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

示例输出（耗时可能有所不同）：

```
query "運命の犠牲者": 2 hits in 14.5µs
  1 (1.899) [見解では、謙虚なヴォードヴィリアンのベテランは、<mark>運命</mark><mark>の</mark><mark>犠</mark><mark>牲</mark><mark>者</mark>と悪役<mark>の</mark>両方<mark>の</mark>変遷として代償を払っています]
  3 (1.837) [見解では、謙虚なヴォードヴィリアンのベテランは、<mark>運命</mark><mark>の</mark><mark>犠</mark><mark>牲</mark><mark>者</mark>と悪役<mark>の</mark>両方<mark>の</mark>変遷として代償を払っています浮き沈み]
query "搜索引擎": 1 hits in 19.792µs
  5 (1.531) [Riot 是用 Go 语言编写的全文<mark>搜索</mark><mark>引擎</mark>]
query "vaudevillian": 1 hits in 1.958µs
  4 (0.654) [In view, humble <mark>vaudevillian</mark> veteran cast vicariously as both victim and villain vicissitudes of fate.]
```

### 使用 gse 为结构体建立索引

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

输出：

```text
article-1: Getting started [Getting <mark>started</mark>]
```

### 向量搜索：精确 KNN 和近似 ANN

使用 `go run ./examples/knn` 运行 [`examples/knn`](../examples/knn)：

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

输出：

```text
KNN: east (1.000)
KNN: northeast (0.707)
ANN: east (1.000)
ANN: northeast (0.707)
```

### 持久化向量索引

HNSW 图随磁盘段一起保存，重新打开索引后无需重建即可搜索。`WithVectorStorage` 选择向量的存放位置：`riot.VectorStorageDisk`（默认）将图连同向量内存映射，向量位于操作系统页缓存而非 Go 堆中；`riot.VectorStorageMemory` 只保存连接，打开图时将向量读入堆（文件更小，内存占用更多）。

```go
config := riot.DefaultConfig("./riot_vectors").WithVectorStorage(riot.VectorStorageDisk)
```

[`examples/ann_disk`](../examples/ann_disk) 在磁盘上索引向量，并在重新打开后搜索：`go run ./examples/ann_disk`。

## 基准测试

### Riot 与 Bluge（其分叉来源）对比

同一个 Go 程序分别基于 `blugelabs/bluge v0.2.2`（最新标签）和 `vcaesar/riot v1.61.2` 构建，仅导入路径不同：200,000 篇磁盘文档（每篇 12 个随机词、一个关键词和一个数字），每个查询运行 50 次（取前 10 条并统计命中数），取三轮交替运行的中位数（Apple M1，负载均值 2.5 至 3.3）。

| 负载 | 命中数 | Bluge v0.2.2 | Riot v1.61.2 | Riot 对比 Bluge |
| --- | --- | --- | --- | --- |
| 索引 200,000 篇文档并关闭 | | 4.83 s | 2.78 s | 快 1.7 倍 |
| 磁盘索引大小 | | 57.3 MB | 36.4 MB | 小 36% |
| Term | 67,086 | 9.03 ms | 0.31 ms | 快 29 倍 |
| Match，2 个词 | 112,469 | 19.9 ms | 5.65 ms | 快 3.5 倍 |
| Match phrase | 2,435 | 12.8 ms | 6.94 ms | 快 1.8 倍 |
| Bool `must` rag, chunk + `must_not` edge | 14,975 | 7.70 ms | 3.41 ms | 快 2.3 倍 |
| Bool `must` rag, chunk | 21,207 | 6.20 ms | 4.18 ms | 快 1.5 倍 |
| Bool `should` rag, chunk, `min_should` 2 | 21,207 | 8.79 ms | 4.19 ms | 快 2.1 倍 |
| Bool `should` rag, chunk, edge, `min_should` 3 | 6,232 | 10.2 ms | 2.75 ms | 快 3.7 倍 |
| Bool `should` rag, chunk （任一） | 112,377 | 19.7 ms | 5.87 ms | 快 3.4 倍 |
| Prefix `se*`（riot 中为常量评分） | 112,414 | 18.7 ms | 0.18 ms | 快 104 倍 |
| Fuzzy `clustr` | 67,002 | 10.0 ms | 2.66 ms | 快 3.8 倍 |
| 数值范围 | 19,648 | 4.17 ms | 2.46 ms | 快 1.7 倍 |
| Match，按数值字段排序 | 66,690 | 123 ms | 27.4 ms | 快 4.5 倍 |

每一轮 riot 运行都快于每一轮 Bluge 运行，且命中数相同。评分不同是因为 riot 将 Bluge 的 BM25 idf 修正为 Lucene 的 `log(1 + (N − n + 0.5)/(n + 0.5))`。Bluge 不支持向量搜索。以上为随机数据上的单机结果，并非正式基准测试。

<!-- ## Repobeats

![Alt](https://repobeats.axiom.co/api/embed/0d7f8bc7927e15b07f1ae592eeff01811c5a2f80.svg "Repobeats analytics image") -->

## 许可证

Apache License Version 2.0
