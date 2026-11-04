# <img src="../docs/logo_4.svg" alt="Riot" width="48" height="48"> Riot

[![PkgGoDev](https://pkg.go.dev/badge/github.com/vcaesar/riot)](https://pkg.go.dev/github.com/vcaesar/riot)
[![Tests](https://github.com/vcaesar/riot/actions/workflows/tests.yml/badge.svg?branch=main&event=push)](https://github.com/vcaesar/riot/actions/workflows/tests.yml?query=event%3Apush+branch%3Amain)
[![Lint](https://github.com/vcaesar/riot/actions/workflows/lint.yml/badge.svg?branch=main&event=push)](https://github.com/vcaesar/riot/actions/workflows/lint.yml?query=event%3Apush+branch%3Amain)
[![GitHub release](https://img.shields.io/github/release/vcaesar/riot.svg)](https://github.com/vcaesar/riot/releases/latest)
<a href="https://discord.gg/535uZPakJw"><img src="https://img.shields.io/discord/908440023093903432.svg?logo=discord&logoColor=white&label=Discord&color=5865F2" alt="Únete al chat de Discord: https://discord.gg/535uZPakJw"></a>

[English](../README.md) | [简体中文](README.zh.md) | [繁體中文](README.zht.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Français](README.fr.md) | [Deutsch](README.de.md) | Español | [Русский](README.ru.md) | [Português](README.pt.md)

Indexación de texto rápida y moderna en Go, fork de [bluge](https://github.com/blugelabs/bluge)

Para un motor de búsqueda distribuido y una alternativa ligera a Elasticsearch completo basada en riot, consulte [zedsearch](https://github.com/vcaesar/zedsearch).

## Características

- Tipos de campo soportados:
  - Text, Numeric, Date, Boolean, IP, Geo Point, Vector
- Tipos de consulta soportados:
  - Term, Phrase, Match, Match Phrase, Prefix, Regexp, Wildcard, Fuzzy
  - Conjunction, Disjunction, Boolean
  - Numeric Range, Date Range, Term Range, IP Range
  - Geo Bounding Box, Geo Distance, Geo Polygon, ANN, KNN
- Similitud/puntuación BM25 con interfaces intercambiables
- Búsqueda vectorial aproximada HNSW con cuantización int8; los grafos se persisten con los segmentos y se mapean en memoria desde disco
- Resaltado de coincidencias en los resultados de búsqueda
- Agregaciones extensibles:
  - Bucketing
    - Terms
    - Numeric Range
    - Date Range
  - Métricas
    - Min/Max/Count/Sum
    - Avg/Weighted Avg
    - Estimación de cardinalidad ([HyperLogLog++](https://github.com/axiomhq/hyperloglog))
    - Aproximación de cuantiles ([T-Digest](https://github.com/caio/go-tdigest))

## Instalación

```sh
go get -u github.com/vcaesar/riot
```

## Uso

Las versiones ejecutables de los programas de indexación básica, consulta y procesamiento CJK están en
[`test/readme_demo`](../test/readme_demo). Las demostraciones de indexación de estructuras y búsqueda vectorial están en
[`examples`](../examples); ejecuta los comandos de estas demostraciones desde la raíz del repositorio.

### Indexación

Guarda como `write/main.go` y ejecuta con `go run ./write`:

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

Guarda como `read/main.go` y ejecuta con `go run ./read` sobre el índice creado arriba.
Si indexas y buscas en el mismo proceso, usa `writer.Reader()` en lugar de `riot.OpenReader`
(o `riot.InMemoryOnlyConfig()` para un índice en memoria):

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

Salida:

```
match: example
```

### Chino / Japonés con gse

El paquete [`gse`](../gse) envuelve riot con un tokenizador [gse](https://github.com/go-ego/gse) para
texto CJK, además de búsqueda por cadena de consulta y resaltado. Guarda como `cjk/main.go` y ejecuta con
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

Salida de ejemplo; los tiempos pueden variar:

```
query "運命の犠牲者": 2 hits in 14.5µs
  1 (1.899) [見解では、謙虚なヴォードヴィリアンのベテランは、<mark>運命</mark><mark>の</mark><mark>犠</mark><mark>牲</mark><mark>者</mark>と悪役<mark>の</mark>両方<mark>の</mark>変遷として代償を払っています]
  3 (1.837) [見解では、謙虚なヴォードヴィリアンのベテランは、<mark>運命</mark><mark>の</mark><mark>犠</mark><mark>牲</mark><mark>者</mark>と悪役<mark>の</mark>両方<mark>の</mark>変遷として代償を払っています浮き沈み]
query "搜索引擎": 1 hits in 19.792µs
  5 (1.531) [Riot 是用 Go 语言编写的全文<mark>搜索</mark><mark>引擎</mark>]
query "vaudevillian": 1 hits in 1.958µs
  4 (0.654) [In view, humble <mark>vaudevillian</mark> veteran cast vicariously as both victim and villain vicissitudes of fate.]
```

### Indexación de estructuras con gse

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

Salida:

```text
article-1: Getting started [Getting <mark>started</mark>]
```

### Búsqueda vectorial: KNN exacto y ANN aproximado

Ejecuta [`examples/knn`](../examples/knn) con `go run ./examples/knn`:

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

Salida:

```text
KNN: east (1.000)
KNN: northeast (0.707)
ANN: east (1.000)
ANN: northeast (0.707)
```

### Índices vectoriales persistidos

Los grafos HNSW se guardan con los segmentos en disco, así que un índice reabierto los consulta sin reconstruirlos. `WithVectorStorage` elige dónde viven sus vectores: `riot.VectorStorageDisk` (predeterminado) mapea en memoria cada grafo con sus vectores, que quedan en la caché de páginas del SO en lugar del heap de Go; `riot.VectorStorageMemory` guarda solo los enlaces y carga los vectores en el heap al abrir un grafo (archivos más pequeños, más memoria).

```go
config := riot.DefaultConfig("./riot_vectors").WithVectorStorage(riot.VectorStorageDisk)
```

[`examples/ann_disk`](../examples/ann_disk) indexa vectores en disco y los busca tras reabrir el índice: `go run ./examples/ann_disk`.

## Benchmarks

### Riot vs Bleve y Bluge (el motor del que proviene)

Los tres motores ejecutan la misma carga: 100 000 documentos sintéticos (texto `body` con posiciones, almacenado; `id` numérico; palabra clave `category`). Bleve v2.6.1 (scorch, BM25) y Bluge v0.2.2 se comparan con el riot actual (Go 1.27.3) en su modo de indexación por flujo predeterminado, un proceso a la vez en un solo núcleo (`GOMAXPROCS=1`), mediana de 5 rondas rotadas (Apple M1, carga media de 3,1 a 5,4). En cada ejecución los tres devolvieron los mismos números y conjuntos de resultados que un oráculo documento a documento.

| Carga de trabajo | Bleve v2.6.1 | Bluge v0.2.2 | Riot | Riot vs Bleve | Riot vs Bluge |
| --- | --- | --- | --- | --- | --- |
| Indexación inicial, 100 000 docs | 6,546 ms | 5,029 ms | 516 ms | 12.7× más rápido | 9.8× más rápido |
| Commit de 1 documento + lector visible | 0.151 ms | 0.313 ms | 0.199 ms§ | 1.3× más lento | 1.6× más rápido |
| Tamaño del índice en disco | 76.71 MB | 61.06 MB | 8.22 MB | 9.3× más pequeño | 7.4× más pequeño |
| Memoria máxima (RSS) | 1,931 MiB | 1,974 MiB | 144 MiB | 13.4× menos | 13.7× menos |
| CPU total, ejecución completa | 17.84 s | 19.50 s | 1.08 s | 16.5× menos | 18.1× menos |
| Término raro | 11.90 µs | 16.98 µs | 2.21 µs | 5.4× más rápido | 7.7× más rápido |
| Término común, top 10 + recuento | 5,848 µs | 10,861 µs | 7.30 µs† | 801× más rápido | 1,488× más rápido |
| Boolean AND | 1,788 µs | 1,990 µs | 25.38 µs† | 70× más rápido | 78× más rápido |
| Boolean OR | 27.18 µs | 43.61 µs | 6.91 µs | 3.9× más rápido | 6.3× más rápido |
| Frase | 60,645 µs | 61,758 µs | 2,262 µs | 27× más rápido | 27× más rápido |
| Prefijo `term12*` (puntuación constante en riot) | 3,133 µs | 3,597 µs | 50.18 µs | 62× más rápido | 72× más rápido |
| Fuzzy `serch`, distancia 1 | 6,798 µs | 12,035 µs | 29.98 µs† | 227× más rápido | 401× más rápido |
| Rango numérico tipado | 166.2 µs | 164.6 µs | 10.56 µs | 16× más rápido | 16× más rápido |
| Término de palabra clave / faceta | 385.1 µs | 645.7 µs | 2.98 µs | 129× más rápido | 217× más rápido |
| Estadísticas numéricas‡ | 45,375 µs | 105,356 µs | 433.6 µs | 105× más rápido | 243× más rápido |

- La memoria máxima y la CPU cubren todo el proceso de benchmark (construcciones de 10K y 100K y todos los bucles de consultas), tomadas del rusage de `wait4`. El tamaño del índice procede de una construcción en disco aparte (mediana de 3).

Las puntuaciones difieren de las de Bluge porque riot corrigió el idf de BM25 de Bluge al de Lucene: `log(1 + (N − n + 0.5)/(n + 0.5))`. Bluge no tiene búsqueda vectorial; la de Bleve requiere la etiqueta de compilación `vectors` y FAISS mediante cgo. Son cifras de una sola máquina con datos sintéticos, no un benchmark formal.

## Licencia

Apache License Version 2.0
