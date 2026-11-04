# <img src="../docs/logo_4.svg" alt="Riot" width="48" height="48"> Riot

[![PkgGoDev](https://pkg.go.dev/badge/github.com/vcaesar/riot)](https://pkg.go.dev/github.com/vcaesar/riot)
[![Tests](https://github.com/vcaesar/riot/actions/workflows/tests.yml/badge.svg?branch=main&event=push)](https://github.com/vcaesar/riot/actions/workflows/tests.yml?query=event%3Apush+branch%3Amain)
[![Lint](https://github.com/vcaesar/riot/actions/workflows/lint.yml/badge.svg?branch=main&event=push)](https://github.com/vcaesar/riot/actions/workflows/lint.yml?query=event%3Apush+branch%3Amain)
[![GitHub release](https://img.shields.io/github/release/vcaesar/riot.svg)](https://github.com/vcaesar/riot/releases/latest)
<a href="https://discord.gg/535uZPakJw"><img src="https://img.shields.io/discord/908440023093903432.svg?logo=discord&logoColor=white&label=Discord&color=5865F2" alt="加入 Discord 聊天: https://discord.gg/535uZPakJw"></a>

[English](../README.md) | [简体中文](README.zh.md) | 繁體中文 | [日本語](README.ja.md) | [한국어](README.ko.md) | [Français](README.fr.md) | [Deutsch](README.de.md) | [Español](README.es.md) | [Русский](README.ru.md) | [Português](README.pt.md)

快速、現代的 Go 語言全文索引函式庫，fork 自 [bluge](https://github.com/blugelabs/bluge)

如需基於 riot 的分散式搜尋引擎、完整 Elasticsearch 的輕量替代方案，請參閱 [zedsearch](https://github.com/vcaesar/zedsearch)。

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

### Riot 與 Bleve、Bluge（其分叉來源）對比

三個引擎執行同一負載：100,000 篇合成文件（帶位置並儲存的 `body` 文字、數值 `id`、`category` 關鍵字）。Bleve v2.6.1（scorch，BM25）和 Bluge v0.2.2 對比目前的 riot（Go 1.27.3，預設的串流索引模式），每次只執行一個行程，單核（`GOMAXPROCS=1`），取輪換 5 輪的中位數（Apple M1，負載平均 3.1 至 5.4）。每次執行中三者的命中數和命中集合都與逐文件校驗結果一致。

| 工作負載 | Bleve v2.6.1 | Bluge v0.2.2 | Riot | Riot 對比 Bleve | Riot 對比 Bluge |
| --- | --- | --- | --- | --- | --- |
| 初始索引，100,000 篇文件 | 6,546 ms | 5,029 ms | 516 ms | 快 12.7 倍 | 快 9.8 倍 |
| 單文件提交 + 可見 reader | 0.151 ms | 0.313 ms | 0.199 ms§ | 慢 1.3 倍 | 快 1.6 倍 |
| 磁碟索引大小 | 76.71 MB | 61.06 MB | 8.22 MB | 小 9.3 倍 | 小 7.4 倍 |
| 峰值記憶體（RSS） | 1,931 MiB | 1,974 MiB | 144 MiB | 少 13.4 倍 | 少 13.7 倍 |
| 總 CPU，整個執行 | 17.84 s | 19.50 s | 1.08 s | 少 16.5 倍 | 少 18.1 倍 |
| 稀有詞 | 11.90 µs | 16.98 µs | 2.21 µs | 快 5.4 倍 | 快 7.7 倍 |
| 常見詞，前 10 筆 + 計數 | 5,848 µs | 10,861 µs | 7.30 µs† | 快 801 倍 | 快 1,488 倍 |
| Boolean AND | 1,788 µs | 1,990 µs | 25.38 µs† | 快 70 倍 | 快 78 倍 |
| Boolean OR | 27.18 µs | 43.61 µs | 6.91 µs | 快 3.9 倍 | 快 6.3 倍 |
| 短語 | 60,645 µs | 61,758 µs | 2,262 µs | 快 27 倍 | 快 27 倍 |
| 前綴 `term12*`（riot 中為常數評分） | 3,133 µs | 3,597 µs | 50.18 µs | 快 62 倍 | 快 72 倍 |
| 模糊 `serch`，編輯距離 1 | 6,798 µs | 12,035 µs | 29.98 µs† | 快 227 倍 | 快 401 倍 |
| 型別化數值範圍 | 166.2 µs | 164.6 µs | 10.56 µs | 快 16 倍 | 快 16 倍 |
| 關鍵字 / 分面 term | 385.1 µs | 645.7 µs | 2.98 µs | 快 129 倍 | 快 217 倍 |
| 數值統計‡ | 45,375 µs | 105,356 µs | 433.6 µs | 快 105 倍 | 快 243 倍 |

- 峰值記憶體和 CPU 涵蓋整個基準行程（10K 和 100K 建置加所有查詢迴圈），取自 `wait4` rusage。索引大小取自獨立的磁碟建置（3 輪中位數）。

評分與 Bluge 不同是因為 riot 將 Bluge 的 BM25 idf 修正為 Lucene 的 `log(1 + (N − n + 0.5)/(n + 0.5))`。Bluge 不支援向量搜尋；Bleve 的向量搜尋需要 `vectors` 建置標籤和經由 cgo 的 FAISS。以上為合成資料上的單機結果，並非正式基準測試。

## 授權條款

Apache License Version 2.0
