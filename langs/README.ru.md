# <img src="../docs/logo_4.svg" alt="Riot" width="48" height="48"> Riot

[![PkgGoDev](https://pkg.go.dev/badge/github.com/vcaesar/riot)](https://pkg.go.dev/github.com/vcaesar/riot)
[![Tests](https://github.com/vcaesar/riot/actions/workflows/tests.yml/badge.svg?branch=main&event=push)](https://github.com/vcaesar/riot/actions/workflows/tests.yml?query=event%3Apush+branch%3Amain)
[![Lint](https://github.com/vcaesar/riot/actions/workflows/lint.yml/badge.svg?branch=main&event=push)](https://github.com/vcaesar/riot/actions/workflows/lint.yml?query=event%3Apush+branch%3Amain)
[![GoDoc](https://godoc.org/github.com/vcaesar/riot?status.svg)](https://godoc.org/github.com/vcaesar/riot)
[![GitHub release](https://img.shields.io/github/release/vcaesar/riot.svg)](https://github.com/vcaesar/riot/releases/latest)

[English](../README.md) | [简体中文](README.zh.md) | [繁體中文](README.zht.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Français](README.fr.md) | [Deutsch](README.de.md) | [Español](README.es.md) | Русский | [Português](README.pt.md)

Быстрая современная библиотека полнотекстового индексирования на Go, форк [bluge](https://github.com/blugelabs/bluge)

Распределённый поисковый движок и лёгкая альтернатива полноценному Elasticsearch на базе riot — [zed](https://github.com/vcaesar/zedsearch).

## Возможности

- Поддерживаемые типы полей:
  - Text, Numeric, Date, Boolean, IP, Geo Point, Vector
- Поддерживаемые типы запросов:
  - Term, Phrase, Match, Match Phrase, Prefix, Regexp, Wildcard, Fuzzy
  - Conjunction, Disjunction, Boolean
  - Numeric Range, Date Range, Term Range, IP Range
  - Geo Bounding Box, Geo Distance, Geo Polygon, ANN, KNN
- Ранжирование BM25 с подключаемыми интерфейсами
- Приближённый векторный поиск HNSW с квантованием int8; графы сохраняются вместе с сегментами и отображаются в память с диска
- Подсветка совпадений в результатах поиска
- Расширяемые агрегации:
  - Бакетинг
    - Terms
    - Numeric Range
    - Date Range
  - Метрики
    - Min/Max/Count/Sum
    - Avg/Weighted Avg
    - Оценка кардинальности ([HyperLogLog++](https://github.com/axiomhq/hyperloglog))
    - Приближённые квантили ([T-Digest](https://github.com/caio/go-tdigest))

## Установка

```sh
go get -u github.com/vcaesar/riot
```

## Использование

Запускаемые версии базовых программ индексирования, поиска и работы с CJK находятся в
[`test/readme_demo`](../test/readme_demo). Примеры индексирования структур и векторного поиска находятся в
[`examples`](../examples); выполняйте команды для этих примеров из корня репозитория.

### Индексирование

Сохраните как `write/main.go` и запустите `go run ./write`:

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

### Поиск

Сохраните как `read/main.go` и запустите `go run ./read` для индекса, созданного выше.
Если индексирование и поиск выполняются в одном процессе, используйте `writer.Reader()` вместо `riot.OpenReader`
(или `riot.InMemoryOnlyConfig()` для индекса в памяти):

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

Вывод:

```
match: example
```

### Китайский / японский с gse

Пакет [`gse`](../gse) оборачивает riot токенизатором [gse](https://github.com/go-ego/gse) для
CJK-текста и добавляет поиск по строке запроса и подсветку. Сохраните как `cjk/main.go` и запустите
`go run ./cjk`:

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

Пример вывода; время выполнения может отличаться:

```
query "運命の犠牲者": 2 hits in 14.5µs
  1 (1.899) [見解では、謙虚なヴォードヴィリアンのベテランは、<mark>運命</mark><mark>の</mark><mark>犠</mark><mark>牲</mark><mark>者</mark>と悪役<mark>の</mark>両方<mark>の</mark>変遷として代償を払っています]
  3 (1.837) [見解では、謙虚なヴォードヴィリアンのベテランは、<mark>運命</mark><mark>の</mark><mark>犠</mark><mark>牲</mark><mark>者</mark>と悪役<mark>の</mark>両方<mark>の</mark>変遷として代償を払っています浮き沈み]
query "搜索引擎": 1 hits in 19.792µs
  5 (1.531) [Riot 是用 Go 语言编写的全文<mark>搜索</mark><mark>引擎</mark>]
query "vaudevillian": 1 hits in 1.958µs
  4 (0.654) [In view, humble <mark>vaudevillian</mark> veteran cast vicariously as both victim and villain vicissitudes of fate.]
```

### Индексирование структур с gse

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

Вывод:

```text
article-1: Getting started [Getting <mark>started</mark>]
```

### Векторный поиск: точный KNN и приближённый ANN

Запустите [`examples/knn`](../examples/knn) командой `go run ./examples/knn`:

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

Вывод:

```text
KNN: east (1.000)
KNN: northeast (0.707)
ANN: east (1.000)
ANN: northeast (0.707)
```

### Сохраняемые векторные индексы

Графы HNSW сохраняются вместе с сегментами на диске, поэтому повторно открытый индекс ищет по ним без перестроения. `WithVectorStorage` выбирает, где хранятся векторы: `riot.VectorStorageDisk` (по умолчанию) отображает каждый граф вместе с векторами в память, и они находятся в страничном кэше ОС, а не в куче Go; `riot.VectorStorageMemory` хранит только связи и загружает векторы в кучу при открытии графа (файлы меньше, памяти больше).

```go
config := riot.DefaultConfig("./riot_vectors").WithVectorStorage(riot.VectorStorageDisk)
```

[`examples/ann_disk`](../examples/ann_disk) индексирует векторы на диске и ищет по ним после повторного открытия: `go run ./examples/ann_disk`.

## Бенчмарки

### Riot против Bluge (движка, от которого он отделился)

Одна и та же программа на Go, собранная с `blugelabs/bluge v0.2.2` (последний тег) и `vcaesar/riot v1.61.2`, отличается только путём импорта: 200 000 документов на диске (12 случайных слов, ключевое слово и число), каждый запрос выполняется 50 раз (топ-10 с подсчётом совпадений), медиана трёх чередующихся прогонов (Apple M1, средняя нагрузка от 2,5 до 3,3).

| Нагрузка | Совпадения | Bluge v0.2.2 | Riot v1.61.2 | Riot против Bluge |
| --- | --- | --- | --- | --- |
| Индексация 200 000 документов и закрытие | | 4.83 s | 2.78 s | в 1.7 раза быстрее |
| Размер индекса на диске | | 57.3 MB | 36.4 MB | на 36% меньше |
| Term | 67,086 | 9.03 ms | 0.31 ms | в 29 раз быстрее |
| Match, 2 слова | 112,469 | 19.9 ms | 5.65 ms | в 3.5 раза быстрее |
| Match phrase | 2,435 | 12.8 ms | 6.94 ms | в 1.8 раза быстрее |
| Bool `must` rag, chunk + `must_not` edge | 14,975 | 7.70 ms | 3.41 ms | в 2.3 раза быстрее |
| Bool `must` rag, chunk | 21,207 | 6.20 ms | 4.18 ms | в 1.5 раза быстрее |
| Bool `should` rag, chunk, `min_should` 2 | 21,207 | 8.79 ms | 4.19 ms | в 2.1 раза быстрее |
| Bool `should` rag, chunk, edge, `min_should` 3 | 6,232 | 10.2 ms | 2.75 ms | в 3.7 раза быстрее |
| Bool `should` rag, chunk (любое) | 112,377 | 19.7 ms | 5.87 ms | в 3.4 раза быстрее |
| Prefix `se*` (постоянная оценка в riot) | 112,414 | 18.7 ms | 0.18 ms | в 104 раза быстрее |
| Fuzzy `clustr` | 67,002 | 10.0 ms | 2.66 ms | в 3.8 раза быстрее |
| Числовой диапазон | 19,648 | 4.17 ms | 2.46 ms | в 1.7 раза быстрее |
| Match с сортировкой по числовому полю | 66,690 | 123 ms | 27.4 ms | в 4.5 раза быстрее |

Каждый прогон riot оказался быстрее каждого прогона Bluge при одинаковом числе совпадений. Оценки различаются, потому что riot исправил idf в BM25 из Bluge на формулу Lucene `log(1 + (N − n + 0.5)/(n + 0.5))`. В Bluge нет векторного поиска. Это результаты на одной машине со случайными данными, а не формальный бенчмарк.

<!-- ## Repobeats

![Alt](https://repobeats.axiom.co/api/embed/0d7f8bc7927e15b07f1ae592eeff01811c5a2f80.svg "Repobeats analytics image") -->

## Лицензия

Apache License Version 2.0
