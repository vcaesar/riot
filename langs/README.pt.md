# <img src="../docs/logo_4.svg" alt="Riot" width="48" height="48"> Riot

[![PkgGoDev](https://pkg.go.dev/badge/github.com/vcaesar/riot)](https://pkg.go.dev/github.com/vcaesar/riot)
[![Tests](https://github.com/vcaesar/riot/actions/workflows/tests.yml/badge.svg?branch=main&event=push)](https://github.com/vcaesar/riot/actions/workflows/tests.yml?query=event%3Apush+branch%3Amain)
[![Lint](https://github.com/vcaesar/riot/actions/workflows/lint.yml/badge.svg?branch=main&event=push)](https://github.com/vcaesar/riot/actions/workflows/lint.yml?query=event%3Apush+branch%3Amain)
[![GitHub release](https://img.shields.io/github/release/vcaesar/riot.svg)](https://github.com/vcaesar/riot/releases/latest)
<a href="https://discord.gg/535uZPakJw"><img src="https://img.shields.io/discord/908440023093903432.svg?logo=discord&logoColor=white&label=Discord&color=5865F2" alt="Entre no chat do Discord: https://discord.gg/535uZPakJw"></a>

[English](../README.md) | [简体中文](README.zh.md) | [繁體中文](README.zht.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Français](README.fr.md) | [Deutsch](README.de.md) | [Español](README.es.md) | [Русский](README.ru.md) | Português

Indexação de texto rápida e moderna em Go, fork do [bluge](https://github.com/blugelabs/bluge)

Para um mecanismo de busca distribuído e uma alternativa leve ao Elasticsearch completo baseada em riot, veja [zed](https://github.com/vcaesar/zedsearch).

## Funcionalidades

- Tipos de campo suportados:
  - Text, Numeric, Date, Boolean, IP, Geo Point, Vector
- Tipos de consulta suportados:
  - Term, Phrase, Match, Match Phrase, Prefix, Regexp, Wildcard, Fuzzy
  - Conjunction, Disjunction, Boolean
  - Numeric Range, Date Range, Term Range, IP Range
  - Geo Bounding Box, Geo Distance, Geo Polygon, ANN, KNN
- Similaridade/pontuação BM25 com interfaces plugáveis
- Busca vetorial aproximada HNSW com quantização int8; os grafos são persistidos com os segmentos e mapeados em memória a partir do disco
- Realce das correspondências nos resultados de busca
- Agregações extensíveis:
  - Bucketing
    - Terms
    - Numeric Range
    - Date Range
  - Métricas
    - Min/Max/Count/Sum
    - Avg/Weighted Avg
    - Estimativa de cardinalidade ([HyperLogLog++](https://github.com/axiomhq/hyperloglog))
    - Aproximação de quantis ([T-Digest](https://github.com/caio/go-tdigest))

## Instalação

```sh
go get -u github.com/vcaesar/riot
```

## Uso

Versões executáveis dos programas básicos de indexação, consulta e CJK estão em
[`test/readme_demo`](../test/readme_demo). Exemplos de indexação de structs e busca vetorial estão em
[`examples`](../examples); execute os comandos desses exemplos a partir da raiz do repositório.

### Indexação

Salve como `write/main.go` e execute com `go run ./write`:

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

### Consulta

Salve como `read/main.go` e execute com `go run ./read` sobre o índice criado acima.
Se você indexa e busca no mesmo processo, use `writer.Reader()` em vez de `riot.OpenReader`
(ou `riot.InMemoryOnlyConfig()` para um índice em memória):

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

Saída:

```
match: example
```

### Chinês / Japonês com gse

O pacote [`gse`](../gse) envolve o riot com um tokenizador [gse](https://github.com/go-ego/gse) para
texto CJK, além de busca por string de consulta e realce. Salve como `cjk/main.go` e execute com
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

Saída ilustrativa; os tempos podem variar:

```
query "運命の犠牲者": 2 hits in 14.5µs
  1 (1.899) [見解では、謙虚なヴォードヴィリアンのベテランは、<mark>運命</mark><mark>の</mark><mark>犠</mark><mark>牲</mark><mark>者</mark>と悪役<mark>の</mark>両方<mark>の</mark>変遷として代償を払っています]
  3 (1.837) [見解では、謙虚なヴォードヴィリアンのベテランは、<mark>運命</mark><mark>の</mark><mark>犠</mark><mark>牲</mark><mark>者</mark>と悪役<mark>の</mark>両方<mark>の</mark>変遷として代償を払っています浮き沈み]
query "搜索引擎": 1 hits in 19.792µs
  5 (1.531) [Riot 是用 Go 语言编写的全文<mark>搜索</mark><mark>引擎</mark>]
query "vaudevillian": 1 hits in 1.958µs
  4 (0.654) [In view, humble <mark>vaudevillian</mark> veteran cast vicariously as both victim and villain vicissitudes of fate.]
```

### Indexação de structs com gse

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

Saída:

```text
article-1: Getting started [Getting <mark>started</mark>]
```

### Busca vetorial: KNN exato e ANN aproximado

Execute [`examples/knn`](../examples/knn) com `go run ./examples/knn`:

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

Saída:

```text
KNN: east (1.000)
KNN: northeast (0.707)
ANN: east (1.000)
ANN: northeast (0.707)
```

### Índices vetoriais persistidos

Os grafos HNSW são salvos com os segmentos em disco, então um índice reaberto os consulta sem reconstruí-los. `WithVectorStorage` escolhe onde ficam os vetores: `riot.VectorStorageDisk` (padrão) mapeia em memória cada grafo com seus vetores, que ficam no cache de páginas do SO em vez do heap do Go; `riot.VectorStorageMemory` armazena apenas os links e carrega os vetores no heap ao abrir um grafo (arquivos menores, mais memória).

```go
config := riot.DefaultConfig("./riot_vectors").WithVectorStorage(riot.VectorStorageDisk)
```

[`examples/ann_disk`](../examples/ann_disk) indexa vetores em disco e os busca após reabrir o índice: `go run ./examples/ann_disk`.

## Benchmarks

### Riot vs Bluge (o motor do qual foi derivado)

Um mesmo programa Go compilado com `blugelabs/bluge v0.2.2` (tag mais recente) e `vcaesar/riot v1.61.2`, mudando apenas o caminho de importação: 200.000 documentos em disco (12 palavras aleatórias, uma palavra-chave e um número), cada consulta executada 50 vezes (top 10 com contagem de resultados), mediana de três execuções alternadas (Apple M1, carga média de 2,5 a 3,3).

| Carga de trabalho | Resultados | Bluge v0.2.2 | Riot v1.61.2 | Riot vs Bluge |
| --- | --- | --- | --- | --- |
| Indexar 200.000 documentos e fechar | | 4.83 s | 2.78 s | 1.7× mais rápido |
| Tamanho do índice em disco | | 57.3 MB | 36.4 MB | 36% menor |
| Term | 67,086 | 9.03 ms | 0.31 ms | 29× mais rápido |
| Match, 2 palavras | 112,469 | 19.9 ms | 5.65 ms | 3.5× mais rápido |
| Match phrase | 2,435 | 12.8 ms | 6.94 ms | 1.8× mais rápido |
| Bool `must` rag, chunk + `must_not` edge | 14,975 | 7.70 ms | 3.41 ms | 2.3× mais rápido |
| Bool `must` rag, chunk | 21,207 | 6.20 ms | 4.18 ms | 1.5× mais rápido |
| Bool `should` rag, chunk, `min_should` 2 | 21,207 | 8.79 ms | 4.19 ms | 2.1× mais rápido |
| Bool `should` rag, chunk, edge, `min_should` 3 | 6,232 | 10.2 ms | 2.75 ms | 3.7× mais rápido |
| Bool `should` rag, chunk (qualquer) | 112,377 | 19.7 ms | 5.87 ms | 3.4× mais rápido |
| Prefix `se*` (pontuação constante no riot) | 112,414 | 18.7 ms | 0.18 ms | 104× mais rápido |
| Fuzzy `clustr` | 67,002 | 10.0 ms | 2.66 ms | 3.8× mais rápido |
| Intervalo numérico | 19,648 | 4.17 ms | 2.46 ms | 1.7× mais rápido |
| Match, ordenado por um campo numérico | 66,690 | 123 ms | 27.4 ms | 4.5× mais rápido |

Todas as execuções do riot superaram todas as do Bluge, com as mesmas contagens de resultados. As pontuações diferem porque o riot corrigiu o idf do BM25 do Bluge para o do Lucene, `log(1 + (N − n + 0.5)/(n + 0.5))`. O Bluge não tem busca vetorial. São números de uma única máquina com dados aleatórios, não um benchmark formal.

<!-- ## Repobeats

![Alt](https://repobeats.axiom.co/api/embed/0d7f8bc7927e15b07f1ae592eeff01811c5a2f80.svg "Repobeats analytics image") -->

## Licença

Apache License Version 2.0
