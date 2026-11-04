# <img src="../docs/logo_4.svg" alt="Riot" width="48" height="48"> Riot

[![PkgGoDev](https://pkg.go.dev/badge/github.com/vcaesar/riot)](https://pkg.go.dev/github.com/vcaesar/riot)
[![Tests](https://github.com/vcaesar/riot/actions/workflows/tests.yml/badge.svg?branch=main&event=push)](https://github.com/vcaesar/riot/actions/workflows/tests.yml?query=event%3Apush+branch%3Amain)
[![Lint](https://github.com/vcaesar/riot/actions/workflows/lint.yml/badge.svg?branch=main&event=push)](https://github.com/vcaesar/riot/actions/workflows/lint.yml?query=event%3Apush+branch%3Amain)
[![GitHub release](https://img.shields.io/github/release/vcaesar/riot.svg)](https://github.com/vcaesar/riot/releases/latest)
<a href="https://discord.gg/535uZPakJw"><img src="https://img.shields.io/discord/908440023093903432.svg?logo=discord&logoColor=white&label=Discord&color=5865F2" alt="Entre no chat do Discord: https://discord.gg/535uZPakJw"></a>

[English](../README.md) | [简体中文](README.zh.md) | [繁體中文](README.zht.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Français](README.fr.md) | [Deutsch](README.de.md) | [Español](README.es.md) | [Русский](README.ru.md) | Português

Indexação de texto rápida e moderna em Go, fork do [bluge](https://github.com/blugelabs/bluge)

Para um mecanismo de busca distribuído e uma alternativa leve ao Elasticsearch completo baseada em riot, veja [zedsearch](https://github.com/vcaesar/zedsearch).

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

### Riot vs Bleve e Bluge (o motor do qual foi derivado)

Os três motores executam a mesma carga: 100.000 documentos sintéticos (texto `body` com posições, armazenado; `id` numérico; palavra-chave `category`). Bleve v2.6.1 (scorch, BM25) e Bluge v0.2.2 são comparados ao riot atual (Go 1.27.3) no seu modo padrão de indexação por fluxo, um processo por vez em um único núcleo (`GOMAXPROCS=1`), mediana de 5 rodadas em rotação (Apple M1, carga média de 3,1 a 5,4). Em todas as execuções, os três retornaram as mesmas contagens e conjuntos de resultados que um oráculo documento a documento.

| Carga de trabalho | Bleve v2.6.1 | Bluge v0.2.2 | Riot | Riot vs Bleve | Riot vs Bluge |
| --- | --- | --- | --- | --- | --- |
| Indexação inicial, 100.000 docs | 6,546 ms | 5,029 ms | 516 ms | 12.7× mais rápido | 9.8× mais rápido |
| Commit de 1 documento + leitor visível | 0.151 ms | 0.313 ms | 0.199 ms§ | 1.3× mais lento | 1.6× mais rápido |
| Tamanho do índice em disco | 76.71 MB | 61.06 MB | 8.22 MB | 9.3× menor | 7.4× menor |
| Memória máxima (RSS) | 1,931 MiB | 1,974 MiB | 144 MiB | 13.4× menos | 13.7× menos |
| CPU total, execução completa | 17.84 s | 19.50 s | 1.08 s | 16.5× menos | 18.1× menos |
| Termo raro | 11.90 µs | 16.98 µs | 2.21 µs | 5.4× mais rápido | 7.7× mais rápido |
| Termo comum, top 10 + contagem | 5,848 µs | 10,861 µs | 7.30 µs† | 801× mais rápido | 1,488× mais rápido |
| Boolean AND | 1,788 µs | 1,990 µs | 25.38 µs† | 70× mais rápido | 78× mais rápido |
| Boolean OR | 27.18 µs | 43.61 µs | 6.91 µs | 3.9× mais rápido | 6.3× mais rápido |
| Frase | 60,645 µs | 61,758 µs | 2,262 µs | 27× mais rápido | 27× mais rápido |
| Prefixo `term12*` (pontuação constante no riot) | 3,133 µs | 3,597 µs | 50.18 µs | 62× mais rápido | 72× mais rápido |
| Fuzzy `serch`, distância 1 | 6,798 µs | 12,035 µs | 29.98 µs† | 227× mais rápido | 401× mais rápido |
| Intervalo numérico tipado | 166.2 µs | 164.6 µs | 10.56 µs | 16× mais rápido | 16× mais rápido |
| Termo de palavra-chave / faceta | 385.1 µs | 645.7 µs | 2.98 µs | 129× mais rápido | 217× mais rápido |
| Estatísticas numéricas‡ | 45,375 µs | 105,356 µs | 433.6 µs | 105× mais rápido | 243× mais rápido |

- Memória máxima e CPU cobrem todo o processo de benchmark (construções de 10K e 100K mais todos os laços de consulta), obtidos do rusage de `wait4`. O tamanho do índice vem de uma construção em disco separada (mediana de 3).

As pontuações diferem das do Bluge porque o riot corrigiu o idf do BM25 do Bluge para o do Lucene: `log(1 + (N − n + 0.5)/(n + 0.5))`. O Bluge não tem busca vetorial; a do Bleve exige a tag de build `vectors` e FAISS via cgo. São números de uma única máquina com dados sintéticos, não um benchmark formal.

## Licença

Apache License Version 2.0
