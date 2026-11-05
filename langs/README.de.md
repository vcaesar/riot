# <img src="../docs/logo_4.svg" alt="Riot" width="48" height="48"> Riot

[![PkgGoDev](https://pkg.go.dev/badge/github.com/vcaesar/riot)](https://pkg.go.dev/github.com/vcaesar/riot)
[![Tests](https://github.com/vcaesar/riot/actions/workflows/tests.yml/badge.svg?branch=main&event=push)](https://github.com/vcaesar/riot/actions/workflows/tests.yml?query=event%3Apush+branch%3Amain)
[![Lint](https://github.com/vcaesar/riot/actions/workflows/lint.yml/badge.svg?branch=main&event=push)](https://github.com/vcaesar/riot/actions/workflows/lint.yml?query=event%3Apush+branch%3Amain)
[![GitHub release](https://img.shields.io/github/release/vcaesar/riot.svg)](https://github.com/vcaesar/riot/releases/latest)
<a href="https://discord.gg/535uZPakJw"><img src="https://img.shields.io/discord/908440023093903432.svg?logo=discord&logoColor=white&label=Discord&color=5865F2" alt="Dem Discord-Chat beitreten: https://discord.gg/535uZPakJw"></a>

[English](../README.md) | [简体中文](README.zh.md) | [繁體中文](README.zht.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Français](README.fr.md) | Deutsch | [Español](README.es.md) | [Русский](README.ru.md) | [Português](README.pt.md)

Schnelle, moderne Volltextindizierung in Go, Fork von [bluge](https://github.com/blugelabs/bluge)

Eine verteilte Suchmaschine und eine leichtgewichtige Alternative zu einem vollständigen Elasticsearch auf Basis von riot finden Sie in [zedsearch](https://github.com/vcaesar/zedsearch).

## Funktionen

- Unterstützte Feldtypen:
  - Text, Numeric, Date, Boolean, IP, Geo Point, Vector
- Unterstützte Abfragetypen:
  - Term, Phrase, Match, Match Phrase, Prefix, Regexp, Wildcard, Fuzzy
  - Conjunction, Disjunction, Boolean
  - Numeric Range, Date Range, Term Range, IP Range
  - Geo Bounding Box, Geo Distance, Geo Polygon, ANN, KNN
- BM25-Ähnlichkeit/Scoring mit austauschbaren Schnittstellen
- Approximative HNSW-Vektorsuche mit int8-Quantisierung; Graphen werden mit den Segmenten persistiert und von der Festplatte memory-mapped
- Hervorhebung der Treffer in Suchergebnissen
- Erweiterbare Aggregationen:
  - Bucketing
    - Terms
    - Numeric Range
    - Date Range
  - Metriken
    - Min/Max/Count/Sum
    - Avg/Weighted Avg
    - Kardinalitätsschätzung ([HyperLogLog++](https://github.com/axiomhq/hyperloglog))
    - Quantil-Approximation ([T-Digest](https://github.com/caio/go-tdigest))

## Installation

```sh
go get -u github.com/vcaesar/riot
```

## Verwendung

Lauffähige Versionen der Programme zur grundlegenden Indizierung, Abfrage und CJK-Verarbeitung liegen in
[`test/readme_demo`](../test/readme_demo). Demos zur Indizierung von Structs und zur Vektorsuche liegen in
[`examples`](../examples); führen Sie die Befehle für diese Demos im Stammverzeichnis des Repositorys aus.

### Indizieren

Als `write/main.go` speichern und mit `go run ./write` ausführen:

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

### Abfragen

Als `read/main.go` speichern und mit `go run ./read` gegen den oben erstellten Index ausführen.
Wenn Sie im selben Prozess indizieren und suchen, verwenden Sie `writer.Reader()` statt `riot.OpenReader`
(oder `riot.InMemoryOnlyConfig()` für einen In-Memory-Index):

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

Ausgabe:

```
match: example
```

### Chinesisch / Japanisch mit gse

Das Paket [`gse`](../gse) kapselt riot mit einem [gse](https://github.com/go-ego/gse)-Tokenizer für
CJK-Text und bietet zusätzlich Query-String-Suche und Hervorhebung. Als `cjk/main.go` speichern und mit
`go run ./cjk` ausführen:

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

Beispielausgabe; die Laufzeiten können variieren:

```
query "運命の犠牲者": 2 hits in 14.5µs
  1 (1.899) [見解では、謙虚なヴォードヴィリアンのベテランは、<mark>運命</mark><mark>の</mark><mark>犠</mark><mark>牲</mark><mark>者</mark>と悪役<mark>の</mark>両方<mark>の</mark>変遷として代償を払っています]
  3 (1.837) [見解では、謙虚なヴォードヴィリアンのベテランは、<mark>運命</mark><mark>の</mark><mark>犠</mark><mark>牲</mark><mark>者</mark>と悪役<mark>の</mark>両方<mark>の</mark>変遷として代償を払っています浮き沈み]
query "搜索引擎": 1 hits in 19.792µs
  5 (1.531) [Riot 是用 Go 语言编写的全文<mark>搜索</mark><mark>引擎</mark>]
query "vaudevillian": 1 hits in 1.958µs
  4 (0.654) [In view, humble <mark>vaudevillian</mark> veteran cast vicariously as both victim and villain vicissitudes of fate.]
```

### Structs mit gse indizieren

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

Ausgabe:

```text
article-1: Getting started [Getting <mark>started</mark>]
```

### Vektorsuche: exaktes KNN und approximatives ANN

Führen Sie [`examples/knn`](../examples/knn) mit `go run ./examples/knn` aus:

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

Ausgabe:

```text
KNN: east (1.000)
KNN: northeast (0.707)
ANN: east (1.000)
ANN: northeast (0.707)
```

### Persistierte Vektorindizes

HNSW-Graphen werden mit den Segmenten auf der Festplatte gespeichert, sodass ein erneut geöffneter Index sie ohne Neuaufbau durchsucht. `WithVectorStorage` legt fest, wo ihre Vektoren liegen: `riot.VectorStorageDisk` (Standard) mappt jeden Graphen samt Vektoren in den Speicher, sodass diese im Page-Cache des Betriebssystems statt im Go-Heap liegen; `riot.VectorStorageMemory` speichert nur die Verknüpfungen und lädt die Vektoren beim Öffnen eines Graphen in den Heap (kleinere Dateien, mehr Speicher).

```go
config := riot.DefaultConfig("./riot_vectors").WithVectorStorage(riot.VectorStorageDisk)
```

[`examples/ann_disk`](../examples/ann_disk) indexiert Vektoren auf der Festplatte und durchsucht sie nach dem erneuten Öffnen: `go run ./examples/ann_disk`.

## Benchmarks

### Riot vs. Bleve und Bluge (die Engine, von der es geforkt wurde)

Alle drei Engines laufen mit derselben Last: 100.000 synthetische Dokumente (`body`-Text mit Positionen, gespeichert; numerisches `id`; Keyword `category`). Bleve v2.6.2 (scorch, BM25) und Bluge v0.2.2 treten gegen das aktuelle riot (Go 1.27.3) in seinem Standard-Stream-Indizierungsmodus an, jeweils ein Prozess auf einem Kern (`GOMAXPROCS=1`), Median aus 5 rotierenden Runden (Apple M1, Load Average 3,1 bis 5,4). In jedem Lauf lieferten alle drei dieselben Trefferzahlen und Treffermengen wie ein dokumentweises Orakel. Die riot-Spalte wurde am 2026-11-05 mit demselben Harness neu gemessen (Load Average 2,5 bis 2,7); die Werte der anderen Engines stammen aus dem früheren Lauf.

| Workload | Bleve v2.6.2 | Bluge v0.2.2 | Riot | Riot vs. Bleve | Riot vs. Bluge |
| --- | --- | --- | --- | --- | --- |
| Erstindizierung, 100.000 Dokumente | 6,546 ms | 5,029 ms | 500 ms | 13.1× schneller | 10.1× schneller |
| Commit eines Dokuments + sichtbarer Reader | 0.151 ms | 0.313 ms | 0.151 ms§ | ≈ | 2.1× schneller |
| Indexgröße auf der Festplatte | 76.71 MB | 61.06 MB | 8.22 MB | 9.3× kleiner | 7.4× kleiner |
| Spitzenspeicher (RSS) | 1,931 MiB | 1,974 MiB | 144 MiB | 13.4× weniger | 13.7× weniger |
| CPU gesamt, ganzer Lauf | 17.84 s | 19.50 s | 1.05 s | 17.0× weniger | 18.6× weniger |
| Seltener Term | 11.90 µs | 16.98 µs | 2.25 µs | 5.3× schneller | 7.5× schneller |
| Häufiger Term, Top 10 + Anzahl | 5,848 µs | 10,861 µs | 7.17 µs† | 816× schneller | 1,516× schneller |
| Boolean AND | 1,788 µs | 1,990 µs | 27.83 µs† | 64× schneller | 71× schneller |
| Boolean OR | 27.18 µs | 43.61 µs | 4.75 µs | 5.7× schneller | 9.2× schneller |
| Phrase | 60,645 µs | 61,758 µs | 2,175 µs | 28× schneller | 28× schneller |
| Präfix `term12*` (konstanter Score in riot) | 3,133 µs | 3,597 µs | 45.38 µs | 69× schneller | 79× schneller |
| Fuzzy `serch`, Distanz 1 | 6,798 µs | 12,035 µs | 26.17 µs† | 260× schneller | 460× schneller |
| Typisierter numerischer Bereich | 166.2 µs | 164.6 µs | 8.79 µs | 19× schneller | 19× schneller |
| Keyword- / Facetten-Term | 385.1 µs | 645.7 µs | 2.75 µs | 140× schneller | 235× schneller |
| Numerische Statistik‡ | 45,375 µs | 105,356 µs | 433.2 µs | 105× schneller | 243× schneller |

- Spitzenspeicher und CPU umfassen den gesamten Benchmark-Prozess (10K- und 100K-Aufbau plus alle Abfrageschleifen), ermittelt aus `wait4`-rusage. Die Indexgröße stammt aus einem separaten Aufbau auf der Festplatte (Median aus 3).

Die Scores unterscheiden sich von Bluge, weil riot Bluges BM25-idf auf Lucenes `log(1 + (N − n + 0.5)/(n + 0.5))` korrigiert hat. Bluge hat keine Vektorsuche; die von Bleve benötigt das Build-Tag `vectors` und FAISS über cgo. Dies sind Zahlen von einer einzelnen Maschine mit synthetischen Daten, kein formaler Benchmark.

Riot (Go) ist bereits gleichauf mit Tantivy: Im selben Lauf ist es bei 7 von 10 Abfrage-Workloads schneller. Eine noch schnellere Zig-Version von riot ist ebenfalls in Entwicklung. Sie ist noch Beta und noch nicht veröffentlicht. Im selben Benchmark-Lauf war sie bei den meisten Abfragen etwa 2–5× schneller als riot (Go), bei der Indizierung 2.8× schneller und bei allen Abfrage-Workloads mindestens 1.5× schneller als Tantivy (bei den meisten 2–8×).

## Lizenz

Apache License Version 2.0
