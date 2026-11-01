# <img src="../docs/logo_4.svg" alt="Riot" width="48" height="48"> Riot

[![PkgGoDev](https://pkg.go.dev/badge/github.com/vcaesar/riot)](https://pkg.go.dev/github.com/vcaesar/riot)
[![Tests](https://github.com/vcaesar/riot/actions/workflows/tests.yml/badge.svg?branch=main&event=push)](https://github.com/vcaesar/riot/actions/workflows/tests.yml?query=event%3Apush+branch%3Amain)
[![Lint](https://github.com/vcaesar/riot/actions/workflows/lint.yml/badge.svg?branch=main&event=push)](https://github.com/vcaesar/riot/actions/workflows/lint.yml?query=event%3Apush+branch%3Amain)
[![GitHub release](https://img.shields.io/github/release/vcaesar/riot.svg)](https://github.com/vcaesar/riot/releases/latest)
<a href="https://discord.gg/535uZPakJw"><img src="https://img.shields.io/discord/908440023093903432.svg?logo=discord&logoColor=white&label=Discord&color=5865F2" alt="加入 Discord 聊天: https://discord.gg/535uZPakJw"></a>

[English](../README.md) | [简体中文](README.zh.md) | 繁體中文 | [日本語](README.ja.md) | [한국어](README.ko.md) | [Français](README.fr.md) | [Deutsch](README.de.md) | [Español](README.es.md) | [Русский](README.ru.md) | [Português](README.pt.md)

快速、現代的 Go 語言全文索引函式庫，fork 自 [bluge](https://github.com/blugelabs/bluge)

如需基於 riot 的分散式搜尋引擎、完整 Elasticsearch 的輕量替代方案，請參閱 [zed](https://github.com/vcaesar/zedsearch)。

## 特性

- 支援的欄位型別：
  - 文字（Text）、數值（Numeric）、日期（Date）、布林（Boolean）、IP、地理座標（Geo Point）、向量（Vector）
- 支援的查詢型別：
  - Term、Phrase、Match、Match Phrase、Prefix、Regexp、Wildcard、Fuzzy
  - Conjunction、Disjunction、Boolean
  - 數值範圍（Numeric Range）、日期範圍（Date Range）、詞項範圍（Term Range）、IP 範圍（IP Range）
  - 地理矩形（Geo Bounding Box）、地理距離（Geo Distance）、地理多邊形（Geo Polygon）、ANN、KNN
- BM25 相似度/評分，介面可插拔
- HNSW 近似向量搜尋，支援 int8 量化；圖隨段持久化，並從磁碟記憶體映射
- 搜尋結果比對高亮
- 可擴充的聚合：
  - 分桶（Bucketing）
    - Terms
    - 數值範圍
    - 日期範圍
  - 指標（Metrics）
    - Min/Max/Count/Sum
    - Avg/Weighted Avg
    - 基數估計（[HyperLogLog++](https://github.com/axiomhq/hyperloglog)）
    - 分位數近似（[T-Digest](https://github.com/caio/go-tdigest)）

## 安裝

```sh
go get -u github.com/vcaesar/riot
```

## 使用

基本索引、查詢和 CJK 程式的可執行版本位於 [`test/readme_demo`](../test/readme_demo)。
結構體索引和向量搜尋範例位於 [`examples`](../examples)；請在儲存庫根目錄執行這些範例的命令。

### 建立索引

儲存為 `write/main.go`，然後執行 `go run ./write`：

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

### 查詢

儲存為 `read/main.go`，然後針對上面寫入的索引執行 `go run ./read`。
如果在同一個程序中既建索引又搜尋，請使用 `writer.Reader()` 而不是 `riot.OpenReader`
（或者使用 `riot.InMemoryOnlyConfig()` 建立記憶體索引）：

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

輸出：

```
match: example
```

### 使用 gse 處理中文 / 日文

[`gse`](../gse) 套件用 [gse](https://github.com/go-ego/gse) 分詞器封裝了 riot，
用於 CJK 文字，並提供查詢字串搜尋和高亮。儲存為 `cjk/main.go`，然後執行 `go run ./cjk`：

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

範例輸出（耗時可能有所不同）：

```
query "運命の犠牲者": 2 hits in 14.5µs
  1 (1.899) [見解では、謙虚なヴォードヴィリアンのベテランは、<mark>運命</mark><mark>の</mark><mark>犠</mark><mark>牲</mark><mark>者</mark>と悪役<mark>の</mark>両方<mark>の</mark>変遷として代償を払っています]
  3 (1.837) [見解では、謙虚なヴォードヴィリアンのベテランは、<mark>運命</mark><mark>の</mark><mark>犠</mark><mark>牲</mark><mark>者</mark>と悪役<mark>の</mark>両方<mark>の</mark>変遷として代償を払っています浮き沈み]
query "搜索引擎": 1 hits in 19.792µs
  5 (1.531) [Riot 是用 Go 语言编写的全文<mark>搜索</mark><mark>引擎</mark>]
query "vaudevillian": 1 hits in 1.958µs
  4 (0.654) [In view, humble <mark>vaudevillian</mark> veteran cast vicariously as both victim and villain vicissitudes of fate.]
```

### 使用 gse 為結構體建立索引

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

輸出：

```text
article-1: Getting started [Getting <mark>started</mark>]
```

### 向量搜尋：精確 KNN 和近似 ANN

使用 `go run ./examples/knn` 執行 [`examples/knn`](../examples/knn)：

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

輸出：

```text
KNN: east (1.000)
KNN: northeast (0.707)
ANN: east (1.000)
ANN: northeast (0.707)
```

### 持久化向量索引

HNSW 圖隨磁碟段一起儲存，重新開啟索引後無需重建即可搜尋。`WithVectorStorage` 選擇向量的存放位置：`riot.VectorStorageDisk`（預設）將圖連同向量記憶體映射，向量位於作業系統頁快取而非 Go 堆積中；`riot.VectorStorageMemory` 只儲存連結，開啟圖時將向量讀入堆積（檔案更小，記憶體佔用更多）。

```go
config := riot.DefaultConfig("./riot_vectors").WithVectorStorage(riot.VectorStorageDisk)
```

[`examples/ann_disk`](../examples/ann_disk) 在磁碟上索引向量，並在重新開啟後搜尋：`go run ./examples/ann_disk`。

## 基準測試

### Riot 與 Bluge（其分叉來源）對比

同一個 Go 程式分別基於 `blugelabs/bluge v0.2.2`（最新標籤）和 `vcaesar/riot v1.61.2` 建置，僅匯入路徑不同：200,000 篇磁碟文件（每篇 12 個隨機詞、一個關鍵字和一個數字），每個查詢執行 50 次（取前 10 筆並統計命中數），取三輪交替執行的中位數（Apple M1，負載平均 2.5 至 3.3）。

| 工作負載 | 命中數 | Bluge v0.2.2 | Riot v1.61.2 | Riot 對比 Bluge |
| --- | --- | --- | --- | --- |
| 索引 200,000 篇文件並關閉 | | 4.83 s | 2.78 s | 快 1.7 倍 |
| 磁碟索引大小 | | 57.3 MB | 36.4 MB | 小 36% |
| Term | 67,086 | 9.03 ms | 0.31 ms | 快 29 倍 |
| Match，2 個詞 | 112,469 | 19.9 ms | 5.65 ms | 快 3.5 倍 |
| Match phrase | 2,435 | 12.8 ms | 6.94 ms | 快 1.8 倍 |
| Bool `must` rag, chunk + `must_not` edge | 14,975 | 7.70 ms | 3.41 ms | 快 2.3 倍 |
| Bool `must` rag, chunk | 21,207 | 6.20 ms | 4.18 ms | 快 1.5 倍 |
| Bool `should` rag, chunk, `min_should` 2 | 21,207 | 8.79 ms | 4.19 ms | 快 2.1 倍 |
| Bool `should` rag, chunk, edge, `min_should` 3 | 6,232 | 10.2 ms | 2.75 ms | 快 3.7 倍 |
| Bool `should` rag, chunk （任一） | 112,377 | 19.7 ms | 5.87 ms | 快 3.4 倍 |
| Prefix `se*`（riot 中為常數評分） | 112,414 | 18.7 ms | 0.18 ms | 快 104 倍 |
| Fuzzy `clustr` | 67,002 | 10.0 ms | 2.66 ms | 快 3.8 倍 |
| 數值範圍 | 19,648 | 4.17 ms | 2.46 ms | 快 1.7 倍 |
| Match，依數值欄位排序 | 66,690 | 123 ms | 27.4 ms | 快 4.5 倍 |

每一輪 riot 執行都快於每一輪 Bluge 執行，且命中數相同。評分不同是因為 riot 將 Bluge 的 BM25 idf 修正為 Lucene 的 `log(1 + (N − n + 0.5)/(n + 0.5))`。Bluge 不支援向量搜尋。以上為隨機資料上的單機結果，並非正式基準測試。

<!-- ## Repobeats

![Alt](https://repobeats.axiom.co/api/embed/0d7f8bc7927e15b07f1ae592eeff01811c5a2f80.svg "Repobeats analytics image") -->

## 授權條款

Apache License Version 2.0
