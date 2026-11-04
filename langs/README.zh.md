# <img src="../docs/logo_4.svg" alt="Riot" width="48" height="48"> Riot

[![PkgGoDev](https://pkg.go.dev/badge/github.com/vcaesar/riot)](https://pkg.go.dev/github.com/vcaesar/riot)
[![Tests](https://github.com/vcaesar/riot/actions/workflows/tests.yml/badge.svg?branch=main&event=push)](https://github.com/vcaesar/riot/actions/workflows/tests.yml?query=event%3Apush+branch%3Amain)
[![Lint](https://github.com/vcaesar/riot/actions/workflows/lint.yml/badge.svg?branch=main&event=push)](https://github.com/vcaesar/riot/actions/workflows/lint.yml?query=event%3Apush+branch%3Amain)
[![GitHub release](https://img.shields.io/github/release/vcaesar/riot.svg)](https://github.com/vcaesar/riot/releases/latest)
<a href="https://discord.gg/535uZPakJw"><img src="https://img.shields.io/discord/908440023093903432.svg?logo=discord&logoColor=white&label=Discord&color=5865F2" alt="加入 Discord 聊天: https://discord.gg/535uZPakJw"></a>

[English](../README.md) | 简体中文 | [繁體中文](README.zht.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Français](README.fr.md) | [Deutsch](README.de.md) | [Español](README.es.md) | [Русский](README.ru.md) | [Português](README.pt.md)

快速、现代的 Go 语言全文索引库，fork 自 [bluge](https://github.com/blugelabs/bluge)

如需基于 riot 的分布式搜索引擎、完整 Elasticsearch 的轻量替代方案，请参阅 [zedsearch](https://github.com/vcaesar/zedsearch)。

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

### Riot 与 Bleve、Bluge（其分叉来源）对比

三个引擎运行同一负载：100,000 篇合成文档（带位置并存储的 `body` 文本、数值 `id`、`category` 关键词）。Bleve v2.6.1（scorch，BM25）和 Bluge v0.2.2 对比当前 riot（Go 1.27.3，默认的流式索引模式），每次只运行一个进程，单核（`GOMAXPROCS=1`），取轮换 5 轮的中位数（Apple M1，负载均值 3.1 至 5.4）。每次运行中三者的命中数和命中集合都与逐文档校验结果一致。

| 负载 | Bleve v2.6.1 | Bluge v0.2.2 | Riot | Riot 对比 Bleve | Riot 对比 Bluge |
| --- | --- | --- | --- | --- | --- |
| 初始索引，100,000 篇文档 | 6,546 ms | 5,029 ms | 516 ms | 快 12.7 倍 | 快 9.8 倍 |
| 单文档提交 + 可见 reader | 0.151 ms | 0.313 ms | 0.199 ms§ | 慢 1.3 倍 | 快 1.6 倍 |
| 磁盘索引大小 | 76.71 MB | 61.06 MB | 8.22 MB | 小 9.3 倍 | 小 7.4 倍 |
| 峰值内存（RSS） | 1,931 MiB | 1,974 MiB | 144 MiB | 少 13.4 倍 | 少 13.7 倍 |
| 总 CPU，整个运行 | 17.84 s | 19.50 s | 1.08 s | 少 16.5 倍 | 少 18.1 倍 |
| 稀有词 | 11.90 µs | 16.98 µs | 2.21 µs | 快 5.4 倍 | 快 7.7 倍 |
| 常见词，前 10 条 + 计数 | 5,848 µs | 10,861 µs | 7.30 µs† | 快 801 倍 | 快 1,488 倍 |
| Boolean AND | 1,788 µs | 1,990 µs | 25.38 µs† | 快 70 倍 | 快 78 倍 |
| Boolean OR | 27.18 µs | 43.61 µs | 6.91 µs | 快 3.9 倍 | 快 6.3 倍 |
| 短语 | 60,645 µs | 61,758 µs | 2,262 µs | 快 27 倍 | 快 27 倍 |
| 前缀 `term12*`（riot 中为常量评分） | 3,133 µs | 3,597 µs | 50.18 µs | 快 62 倍 | 快 72 倍 |
| 模糊 `serch`，编辑距离 1 | 6,798 µs | 12,035 µs | 29.98 µs† | 快 227 倍 | 快 401 倍 |
| 类型化数值范围 | 166.2 µs | 164.6 µs | 10.56 µs | 快 16 倍 | 快 16 倍 |
| 关键词 / 分面 term | 385.1 µs | 645.7 µs | 2.98 µs | 快 129 倍 | 快 217 倍 |
| 数值统计‡ | 45,375 µs | 105,356 µs | 433.6 µs | 快 105 倍 | 快 243 倍 |

- 峰值内存和 CPU 覆盖整个基准进程（10K 和 100K 构建加所有查询循环），取自 `wait4` rusage。索引大小取自单独的磁盘构建（3 轮中位数）。

评分与 Bluge 不同是因为 riot 将 Bluge 的 BM25 idf 修正为 Lucene 的 `log(1 + (N − n + 0.5)/(n + 0.5))`。Bluge 不支持向量搜索；Bleve 的向量搜索需要 `vectors` 构建标签和经由 cgo 的 FAISS。以上为合成数据上的单机结果，并非正式基准测试。

## 许可证

Apache License Version 2.0
