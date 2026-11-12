# <img src="../docs/logo_4.svg" alt="Riot" width="48" height="48"> Riot

[![PkgGoDev](https://pkg.go.dev/badge/github.com/vcaesar/riot)](https://pkg.go.dev/github.com/vcaesar/riot)
[![Tests](https://github.com/vcaesar/riot/actions/workflows/tests.yml/badge.svg?branch=main&event=push)](https://github.com/vcaesar/riot/actions/workflows/tests.yml?query=event%3Apush+branch%3Amain)
[![Lint](https://github.com/vcaesar/riot/actions/workflows/lint.yml/badge.svg?branch=main&event=push)](https://github.com/vcaesar/riot/actions/workflows/lint.yml?query=event%3Apush+branch%3Amain)
[![GitHub release](https://img.shields.io/github/release/vcaesar/riot.svg)](https://github.com/vcaesar/riot/releases/latest)
<a href="https://discord.gg/535uZPakJw"><img src="https://img.shields.io/discord/908440023093903432.svg?logo=discord&logoColor=white&label=Discord&color=5865F2" alt="Rejoindre le chat Discord: https://discord.gg/535uZPakJw"></a>

[English](../README.md) | [简体中文](README.zh.md) | [繁體中文](README.zht.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | Français | [Deutsch](README.de.md) | [Español](README.es.md) | [Русский](README.ru.md) | [Português](README.pt.md)

Bibliothèque d'indexation de texte rapide et moderne en Go, fork de [bluge](https://github.com/blugelabs/bluge)

Pour un moteur de recherche distribué et une alternative légère à Elasticsearch complet basée sur riot, voir [zedsearch](https://github.com/vcaesar/zedsearch).

## Fonctionnalités

- Types de champs pris en charge :
  - Text, Numeric, Date, Boolean, IP, Geo Point, Vector
- Types de requêtes pris en charge :
  - Term, Phrase, Match, Match Phrase, Prefix, Regexp, Wildcard, Fuzzy
  - Conjunction, Disjunction, Boolean
  - Numeric Range, Date Range, Term Range, IP Range
  - Geo Bounding Box, Geo Distance, Geo Polygon, ANN, KNN
- Similarité/scoring BM25 avec interfaces interchangeables
- Recherche vectorielle approximative HNSW avec quantification int8 ; les graphes sont persistés avec les segments et mappés en mémoire depuis le disque
- Surlignage des correspondances dans les résultats de recherche
- Agrégations extensibles :
  - Bucketing
    - Terms
    - Numeric Range
    - Date Range
  - Métriques
    - Min/Max/Count/Sum
    - Avg/Weighted Avg
    - Estimation de cardinalité ([HyperLogLog++](https://github.com/axiomhq/hyperloglog))
    - Approximation de quantiles ([T-Digest](https://github.com/caio/go-tdigest))

## Installation

```sh
go get -u github.com/vcaesar/riot
```

## Utilisation

Des versions exécutables des programmes d'indexation de base, de recherche et de traitement CJK se trouvent dans
[`test/readme_demo`](../test/readme_demo). Les démonstrations d'indexation de structures et de recherche vectorielle se trouvent dans
[`examples`](../examples) ; exécutez les commandes de ces démonstrations depuis la racine du dépôt.

### Indexation

Enregistrez sous `write/main.go` et lancez avec `go run ./write` :

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

### Recherche

Enregistrez sous `read/main.go` et lancez avec `go run ./read` sur l'index créé ci-dessus.
Si vous indexez et cherchez dans le même processus, utilisez `writer.Reader()` au lieu de `riot.OpenReader`
(ou `riot.InMemoryOnlyConfig()` pour un index en mémoire) :

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

Sortie :

```
match: example
```

### Chinois / Japonais avec gse

Le paquet [`gse`](../gse) encapsule riot avec un tokenizer [gse](https://github.com/go-ego/gse) pour
le texte CJK, et ajoute la recherche par chaîne de requête et le surlignage. Enregistrez sous `cjk/main.go` et lancez avec
`go run ./cjk` :

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

Exemple de sortie ; les durées peuvent varier :

```
query "運命の犠牲者": 2 hits in 14.5µs
  1 (1.899) [見解では、謙虚なヴォードヴィリアンのベテランは、<mark>運命</mark><mark>の</mark><mark>犠</mark><mark>牲</mark><mark>者</mark>と悪役<mark>の</mark>両方<mark>の</mark>変遷として代償を払っています]
  3 (1.837) [見解では、謙虚なヴォードヴィリアンのベテランは、<mark>運命</mark><mark>の</mark><mark>犠</mark><mark>牲</mark><mark>者</mark>と悪役<mark>の</mark>両方<mark>の</mark>変遷として代償を払っています浮き沈み]
query "搜索引擎": 1 hits in 19.792µs
  5 (1.531) [Riot 是用 Go 语言编写的全文<mark>搜索</mark><mark>引擎</mark>]
query "vaudevillian": 1 hits in 1.958µs
  4 (0.654) [In view, humble <mark>vaudevillian</mark> veteran cast vicariously as both victim and villain vicissitudes of fate.]
```

### Indexation de structures avec gse

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

Sortie :

```text
article-1: Getting started [Getting <mark>started</mark>]
```

### Recherche vectorielle : KNN exact et ANN approximatif

Exécutez [`examples/knn`](../examples/knn) avec `go run ./examples/knn` :

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

Sortie :

```text
KNN: east (1.000)
KNN: northeast (0.707)
ANN: east (1.000)
ANN: northeast (0.707)
```

### Index vectoriels persistés

Les graphes HNSW sont enregistrés avec les segments sur disque : un index rouvert les interroge sans reconstruction. `WithVectorStorage` choisit où vivent leurs vecteurs : `riot.VectorStorageDisk` (par défaut) mappe en mémoire chaque graphe avec ses vecteurs, qui restent dans le cache de pages de l'OS plutôt que dans le tas Go ; `riot.VectorStorageMemory` ne stocke que les liens et charge les vecteurs dans le tas à l'ouverture d'un graphe (fichiers plus petits, plus de mémoire).

```go
config := riot.DefaultConfig("./riot_vectors").WithVectorStorage(riot.VectorStorageDisk)
```

[`examples/ann_disk`](../examples/ann_disk) indexe des vecteurs sur disque et les interroge après réouverture : `go run ./examples/ann_disk`.

## Benchmarks

### Riot vs Bleve et Bluge (le moteur dont il est issu)

Les trois moteurs exécutent la même charge : 100 000 documents synthétiques (texte `body` avec positions, stocké ; `id` numérique ; mot-clé `category`). Bleve v2.6.2 (scorch, BM25) et Bluge v0.2.2 sont comparés à riot actuel (Go 1.27.3) dans son mode d'indexation par flux par défaut, un processus à la fois sur un seul cœur (`GOMAXPROCS=1`), médiane de 5 tours en rotation (Apple M1, charge moyenne de 2,7 à 6,1). À chaque exécution, les trois ont renvoyé les mêmes nombres et ensembles de résultats qu'un oracle document par document.

| Charge | Bleve v2.6.2 | Bluge v0.2.2 | Riot | Riot vs Bleve | Riot vs Bluge |
| --- | --- | --- | --- | --- | --- |
| Indexation initiale, 100 000 docs | 6,546 ms | 5,029 ms | 500 ms | 13.1× plus rapide | 10.1× plus rapide |
| Commit d'un document + lecteur visible | 0.151 ms | 0.313 ms | 0.119 ms§ | 1.3× plus rapide | 2.6× plus rapide |
| Taille de l'index sur disque | 76.71 MB | 61.06 MB | 5.76 MB | 13.3× plus petit | 10.6× plus petit |
| Mémoire maximale (RSS) | 1,931 MiB | 1,974 MiB | 140 MiB | 13.8× moins | 14.1× moins |
| CPU total, exécution complète | 17.84 s | 19.50 s | 1.05 s | 17.0× moins | 18.6× moins |
| Terme rare | 11.90 µs | 16.98 µs | 2.19 µs | 5.4× plus rapide | 7.8× plus rapide |
| Terme courant, top 10 + nombre | 5,848 µs | 10,861 µs | 7.17 µs† | 816× plus rapide | 1,516× plus rapide |
| Boolean AND | 1,788 µs | 1,990 µs | 23.20 µs† | 77× plus rapide | 86× plus rapide |
| Boolean OR | 27.18 µs | 43.61 µs | 4.46 µs | 6.1× plus rapide | 9.8× plus rapide |
| Phrase | 60,645 µs | 61,758 µs | 2,591 µs | 23× plus rapide | 24× plus rapide |
| Préfixe `term12*` (score constant dans riot) | 3,133 µs | 3,597 µs | 35.13 µs | 89× plus rapide | 102× plus rapide |
| Fuzzy `serch`, distance 1 | 6,798 µs | 12,035 µs | 23.70 µs† | 287× plus rapide | 508× plus rapide |
| Plage numérique typée | 166.2 µs | 164.6 µs | 7.84 µs | 21× plus rapide | 21× plus rapide |
| Terme mot-clé / facette | 385.1 µs | 645.7 µs | 2.69 µs | 143× plus rapide | 240× plus rapide |
| Statistiques numériques‡ | 45,375 µs | 105,356 µs | 433.2 µs | 105× plus rapide | 243× plus rapide |

- La mémoire maximale et le CPU couvrent tout le processus de benchmark (constructions 10K et 100K et toutes les boucles de requêtes), issus du rusage de `wait4`. La taille de l'index provient d'une construction sur disque séparée (médiane de 3).

Les scores diffèrent de ceux de Bluge car riot a corrigé l'idf BM25 de Bluge pour utiliser celui de Lucene : `log(1 + (N − n + 0.5)/(n + 0.5))`. Bluge n'a pas de recherche vectorielle ; celle de Bleve exige le tag de build `vectors` et FAISS via cgo. Ce sont des chiffres sur une seule machine avec des données synthétiques, pas un benchmark formel.

Riot (Go) est déjà au niveau de Tantivy : il est plus rapide sur 8 des 10 charges de requêtes dans la même exécution. Une version Zig de riot, encore plus rapide, est aussi en cours de développement. Elle est encore en beta et pas encore publiée. Dans la même exécution de benchmark, elle était environ 2 à 5× plus rapide que riot (Go) sur la plupart des requêtes et 2.8× plus rapide à l'indexation, et au moins 1.5× plus rapide que Tantivy sur toutes les charges de requêtes (2 à 8× sur la plupart).

## Licence

Apache License Version 2.0
