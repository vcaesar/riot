# <img src="../docs/logo_4.svg" alt="Riot" width="48" height="48"> Riot

[![PkgGoDev](https://pkg.go.dev/badge/github.com/vcaesar/riot)](https://pkg.go.dev/github.com/vcaesar/riot)
[![Tests](https://github.com/vcaesar/riot/actions/workflows/tests.yml/badge.svg?branch=main&event=push)](https://github.com/vcaesar/riot/actions/workflows/tests.yml?query=event%3Apush+branch%3Amain)
[![Lint](https://github.com/vcaesar/riot/actions/workflows/lint.yml/badge.svg?branch=main&event=push)](https://github.com/vcaesar/riot/actions/workflows/lint.yml?query=event%3Apush+branch%3Amain)
[![GitHub release](https://img.shields.io/github/release/vcaesar/riot.svg)](https://github.com/vcaesar/riot/releases/latest)
<a href="https://discord.gg/535uZPakJw"><img src="https://img.shields.io/discord/908440023093903432.svg?logo=discord&logoColor=white&label=Discord&color=5865F2" alt="Rejoindre le chat Discord: https://discord.gg/535uZPakJw"></a>

[English](../README.md) | [简体中文](README.zh.md) | [繁體中文](README.zht.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | Français | [Deutsch](README.de.md) | [Español](README.es.md) | [Русский](README.ru.md) | [Português](README.pt.md)

Bibliothèque d'indexation de texte rapide et moderne en Go, fork de [bluge](https://github.com/blugelabs/bluge)

Pour un moteur de recherche distribué et une alternative légère à Elasticsearch complet basée sur riot, voir [zed](https://github.com/vcaesar/zedsearch).

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

### Riot vs Bluge (le moteur dont il est issu)

Un même programme Go compilé avec `blugelabs/bluge v0.2.2` (dernier tag) et `vcaesar/riot v1.61.2`, seul le chemin d'import changeant : 200 000 documents sur disque (12 mots aléatoires, un mot-clé et un nombre), chaque requête exécutée 50 fois (top 10 avec nombre de résultats), médiane de trois exécutions alternées (Apple M1, charge moyenne de 2,5 à 3,3).

| Charge | Résultats | Bluge v0.2.2 | Riot v1.61.2 | Riot vs Bluge |
| --- | --- | --- | --- | --- |
| Indexer 200 000 documents et fermer | | 4.83 s | 2.78 s | 1.7× plus rapide |
| Taille de l'index sur disque | | 57.3 MB | 36.4 MB | 36 % plus petit |
| Term | 67,086 | 9.03 ms | 0.31 ms | 29× plus rapide |
| Match, 2 mots | 112,469 | 19.9 ms | 5.65 ms | 3.5× plus rapide |
| Match phrase | 2,435 | 12.8 ms | 6.94 ms | 1.8× plus rapide |
| Bool `must` rag, chunk + `must_not` edge | 14,975 | 7.70 ms | 3.41 ms | 2.3× plus rapide |
| Bool `must` rag, chunk | 21,207 | 6.20 ms | 4.18 ms | 1.5× plus rapide |
| Bool `should` rag, chunk, `min_should` 2 | 21,207 | 8.79 ms | 4.19 ms | 2.1× plus rapide |
| Bool `should` rag, chunk, edge, `min_should` 3 | 6,232 | 10.2 ms | 2.75 ms | 3.7× plus rapide |
| Bool `should` rag, chunk (au moins un) | 112,377 | 19.7 ms | 5.87 ms | 3.4× plus rapide |
| Prefix `se*` (score constant dans riot) | 112,414 | 18.7 ms | 0.18 ms | 104× plus rapide |
| Fuzzy `clustr` | 67,002 | 10.0 ms | 2.66 ms | 3.8× plus rapide |
| Plage numérique | 19,648 | 4.17 ms | 2.46 ms | 1.7× plus rapide |
| Match, trié par un champ numérique | 66,690 | 123 ms | 27.4 ms | 4.5× plus rapide |

Chaque exécution de riot a battu chaque exécution de Bluge, avec les mêmes nombres de résultats. Les scores diffèrent car riot a corrigé l'idf BM25 de Bluge pour reprendre celui de Lucene, `log(1 + (N − n + 0.5)/(n + 0.5))`. Bluge n'a pas de recherche vectorielle. Ce sont des chiffres mesurés sur une seule machine avec des données aléatoires, pas un benchmark formel.

<!-- ## Repobeats

![Alt](https://repobeats.axiom.co/api/embed/0d7f8bc7927e15b07f1ae592eeff01811c5a2f80.svg "Repobeats analytics image") -->

## Licence

Apache License Version 2.0
