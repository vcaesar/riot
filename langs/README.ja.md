# <img src="../docs/logo_4.svg" alt="Riot" width="48" height="48"> Riot

[![PkgGoDev](https://pkg.go.dev/badge/github.com/vcaesar/riot)](https://pkg.go.dev/github.com/vcaesar/riot)
[![Tests](https://github.com/vcaesar/riot/actions/workflows/tests.yml/badge.svg?branch=main&event=push)](https://github.com/vcaesar/riot/actions/workflows/tests.yml?query=event%3Apush+branch%3Amain)
[![Lint](https://github.com/vcaesar/riot/actions/workflows/lint.yml/badge.svg?branch=main&event=push)](https://github.com/vcaesar/riot/actions/workflows/lint.yml?query=event%3Apush+branch%3Amain)
[![GitHub release](https://img.shields.io/github/release/vcaesar/riot.svg)](https://github.com/vcaesar/riot/releases/latest)
<a href="https://discord.gg/535uZPakJw"><img src="https://img.shields.io/discord/908440023093903432.svg?logo=discord&logoColor=white&label=Discord&color=5865F2" alt="Discord チャットに参加: https://discord.gg/535uZPakJw"></a>

[English](../README.md) | [简体中文](README.zh.md) | [繁體中文](README.zht.md) | 日本語 | [한국어](README.ko.md) | [Français](README.fr.md) | [Deutsch](README.de.md) | [Español](README.es.md) | [Русский](README.ru.md) | [Português](README.pt.md)

Go 製の高速でモダンな全文インデックスライブラリ。[bluge](https://github.com/blugelabs/bluge) からのフォークです。

riot をベースにした分散検索エンジン、フル機能の Elasticsearch の軽量な代替としては [zedsearch](https://github.com/vcaesar/zedsearch) をご覧ください。

## 特徴

- サポートするフィールド型：
  - Text、Numeric、Date、Boolean、IP、Geo Point、Vector
- サポートするクエリ型：
  - Term、Phrase、Match、Match Phrase、Prefix、Regexp、Wildcard、Fuzzy
  - Conjunction、Disjunction、Boolean
  - Numeric Range、Date Range、Term Range、IP Range
  - Geo Bounding Box、Geo Distance、Geo Polygon、ANN、KNN
- BM25 による類似度/スコアリング（インターフェースは差し替え可能）
- int8 量子化による HNSW 近似ベクトル検索。グラフはセグメントと共に永続化され、ディスクからメモリマップされます
- 検索結果のマッチ箇所ハイライト
- 拡張可能な集計：
  - バケット集計
    - Terms
    - Numeric Range
    - Date Range
  - メトリクス
    - Min/Max/Count/Sum
    - Avg/Weighted Avg
    - カーディナリティ推定（[HyperLogLog++](https://github.com/axiomhq/hyperloglog)）
    - 分位点の近似（[T-Digest](https://github.com/caio/go-tdigest)）

## インストール

```sh
go get -u github.com/vcaesar/riot
```

## 使い方

基本的なインデックス作成、検索、CJK プログラムの実行可能版は [`test/readme_demo`](../test/readme_demo) にあります。
構造体のインデックス作成とベクトル検索のデモは [`examples`](../examples) にあります。これらのデモのコマンドはリポジトリのルートから実行してください。

### インデックス作成

`write/main.go` として保存し、`go run ./write` で実行します：

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

### 検索

`read/main.go` として保存し、上で作成したインデックスに対して `go run ./read` で実行します。
同じプロセス内でインデックス作成と検索を行う場合は、`riot.OpenReader` の代わりに `writer.Reader()` を使ってください
（インメモリインデックスなら `riot.InMemoryOnlyConfig()` を使います）：

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

出力：

```
match: example
```

### gse による中国語 / 日本語

[`gse`](../gse) パッケージは、CJK テキスト向けに [gse](https://github.com/go-ego/gse) トークナイザで riot をラップし、
クエリ文字列検索とハイライトも提供します。`cjk/main.go` として保存し、`go run ./cjk` で実行します：

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

出力例（実行時間は変動します）：

```
query "運命の犠牲者": 2 hits in 14.5µs
  1 (1.899) [見解では、謙虚なヴォードヴィリアンのベテランは、<mark>運命</mark><mark>の</mark><mark>犠</mark><mark>牲</mark><mark>者</mark>と悪役<mark>の</mark>両方<mark>の</mark>変遷として代償を払っています]
  3 (1.837) [見解では、謙虚なヴォードヴィリアンのベテランは、<mark>運命</mark><mark>の</mark><mark>犠</mark><mark>牲</mark><mark>者</mark>と悪役<mark>の</mark>両方<mark>の</mark>変遷として代償を払っています浮き沈み]
query "搜索引擎": 1 hits in 19.792µs
  5 (1.531) [Riot 是用 Go 语言编写的全文<mark>搜索</mark><mark>引擎</mark>]
query "vaudevillian": 1 hits in 1.958µs
  4 (0.654) [In view, humble <mark>vaudevillian</mark> veteran cast vicariously as both victim and villain vicissitudes of fate.]
```

### gse による構造体のインデックス作成

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

出力：

```text
article-1: Getting started [Getting <mark>started</mark>]
```

### ベクトル検索：厳密な KNN と近似 ANN

[`examples/knn`](../examples/knn) を `go run ./examples/knn` で実行します：

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

出力：

```text
KNN: east (1.000)
KNN: northeast (0.707)
ANN: east (1.000)
ANN: northeast (0.707)
```

### 永続化されたベクトルインデックス

HNSW グラフはディスク上のセグメントと共に保存されるため、再オープンしたインデックスは再構築なしで検索できます。`WithVectorStorage` はベクトルの保存先を選びます：`riot.VectorStorageDisk`（デフォルト）はグラフをベクトルごとメモリマップし、ベクトルは Go ヒープではなく OS のページキャッシュに置かれます。`riot.VectorStorageMemory` はリンクのみを保存し、グラフを開くときにベクトルをヒープに読み込みます（ファイルは小さく、メモリは多め）。

```go
config := riot.DefaultConfig("./riot_vectors").WithVectorStorage(riot.VectorStorageDisk)
```

[`examples/ann_disk`](../examples/ann_disk) はディスク上でベクトルをインデックスし、再オープン後に検索します：`go run ./examples/ann_disk`。

## ベンチマーク

### Riot と Bleve、Bluge（フォーク元のエンジン）の比較

3 つのエンジンで同じワークロードを実行しました：100,000 件の合成ドキュメント（位置情報付きで保存される `body` テキスト、数値 `id`、`category` キーワード）。Bleve v2.6.2（scorch、BM25）と Bluge v0.2.2 を、デフォルトのストリームインデックスモードの現在の riot（Go 1.27.3）と比較し、一度に 1 プロセスずつ、1 コア（`GOMAXPROCS=1`）、5 ラウンドのローテーションの中央値（Apple M1、ロードアベレージ 2.7〜6.1）を取りました。すべての実行で、3 つのヒット数とヒット集合はドキュメント単位のオラクルと一致しました。

| ワークロード | Bleve v2.6.2 | Bluge v0.2.2 | Riot | Riot と Bleve の比較 | Riot と Bluge の比較 |
| --- | --- | --- | --- | --- | --- |
| 初回インデックス作成、100,000 件 | 6,546 ms | 5,029 ms | 500 ms | 13.1 倍高速 | 10.1 倍高速 |
| 1 件コミット + 可視 reader | 0.151 ms | 0.313 ms | 0.119 ms§ | 1.3 倍高速 | 2.6 倍高速 |
| ディスク上のインデックスサイズ | 76.71 MB | 61.06 MB | 5.76 MB | 13.3 分の 1 | 10.6 分の 1 |
| ピークメモリ（RSS） | 1,931 MiB | 1,974 MiB | 140 MiB | 13.8 分の 1 | 14.1 分の 1 |
| 合計 CPU、実行全体 | 17.84 s | 19.50 s | 1.05 s | 17.0 分の 1 | 18.6 分の 1 |
| 希少語 | 11.90 µs | 16.98 µs | 2.19 µs | 5.4 倍高速 | 7.8 倍高速 |
| 頻出語、上位 10 件 + 件数 | 5,848 µs | 10,861 µs | 7.17 µs† | 816 倍高速 | 1,516 倍高速 |
| Boolean AND | 1,788 µs | 1,990 µs | 23.20 µs† | 77 倍高速 | 86 倍高速 |
| Boolean OR | 27.18 µs | 43.61 µs | 4.46 µs | 6.1 倍高速 | 9.8 倍高速 |
| フレーズ | 60,645 µs | 61,758 µs | 2,591 µs | 23 倍高速 | 24 倍高速 |
| 前方一致 `term12*`（riot では定数スコア） | 3,133 µs | 3,597 µs | 35.13 µs | 89 倍高速 | 102 倍高速 |
| あいまい `serch`、距離 1 | 6,798 µs | 12,035 µs | 23.70 µs† | 287 倍高速 | 508 倍高速 |
| 型付き数値範囲 | 166.2 µs | 164.6 µs | 7.84 µs | 21 倍高速 | 21 倍高速 |
| キーワード / ファセット term | 385.1 µs | 645.7 µs | 2.69 µs | 143 倍高速 | 240 倍高速 |
| 数値統計‡ | 45,375 µs | 105,356 µs | 433.2 µs | 105 倍高速 | 243 倍高速 |

- ピークメモリと CPU はベンチマークプロセス全体（10K と 100K の構築およびすべてのクエリループ）を対象とし、`wait4` の rusage から取得しています。インデックスサイズは別途行ったディスク上の構築（3 回の中央値）によるものです。

Bluge とスコアが異なるのは、riot が Bluge の BM25 idf を Lucene の `log(1 + (N − n + 0.5)/(n + 0.5))` に修正したためです。Bluge はベクトル検索をサポートしていません。Bleve のベクトル検索には `vectors` ビルドタグと cgo 経由の FAISS が必要です。これらは合成データでの単一マシンの数値であり、正式なベンチマークではありません。

riot（Go）はすでに Tantivy と同等で、同じ実行の 10 種類のクエリのうち 8 種類で高速でした。さらに高速な riot の Zig 版も開発中です。まだ beta のため、公開していません。同じベンチマーク実行では、ほとんどのクエリで riot（Go）より約 2〜5 倍高速、インデックス作成は 2.8 倍高速で、すべてのクエリワークロードで Tantivy より 1.5 倍以上高速でした（多くは 2〜8 倍）。

## ライセンス

Apache License Version 2.0
