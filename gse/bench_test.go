//  Copyright (c) 2026 The AtomAI.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 		http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gse

import (
	"strconv"
	"strings"
	"testing"
)

// benchText mixes Chinese prose with Latin words, numbers and punctuation.
var benchText = strings.Repeat("Riot 是用 Go 语言编写的全文搜索引擎，支持中文分词、"+
	"短语查询和 BM25 相关性排序。今天的天气很好，我们在北京大学的图书馆里讨论"+
	"分布式索引的设计，以及如何在 2026 年把查询延迟降低到 3.5 毫秒以内。"+
	"搜索引擎好, 四条腿好, All animals are equal. ", 4)

func BenchmarkTokenizer(b *testing.B) {
	seg, err := NewSegmenter(Option{Dicts: "embed, zh"})
	if err != nil {
		b.Fatal(err)
	}
	input := []byte(benchText)
	for _, mode := range []string{"", "hmm", "search", "search-hmm"} {
		analyzer, err := NewAnalyzer(seg, Option{Opt: mode})
		if err != nil {
			b.Fatal(err)
		}
		b.Run("mode="+mode, func(b *testing.B) {
			b.SetBytes(int64(len(input)))
			b.ReportAllocs()
			for b.Loop() {
				analyzer.Analyze(input)
			}
		})
	}
}

func benchIndex(b *testing.B) *Index {
	b.Helper()
	x, err := New(Option{Dicts: "embed, zh", Opt: "search-hmm"})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := x.Close(); err != nil {
			b.Error(err)
		}
	})
	return x
}

func BenchmarkIndexBatch(b *testing.B) {
	const docs = 200
	x := benchIndex(b)
	b.ReportAllocs()
	for b.Loop() {
		batch := x.Batch()
		for i := range docs {
			if err := batch.Index(strconv.Itoa(i), benchText); err != nil {
				b.Fatal(err)
			}
		}
		if err := batch.Commit(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSearch(b *testing.B) {
	x := benchIndex(b)
	batch := x.Batch()
	for i := range 1000 {
		if err := batch.Index(strconv.Itoa(i), strconv.Itoa(i)+" "+benchText); err != nil {
			b.Fatal(err)
		}
	}
	if err := batch.Commit(); err != nil {
		b.Fatal(err)
	}
	for _, q := range []string{"搜索引擎", "分布式索引", "riot"} {
		b.Run(q, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				res, err := x.Search(QueryString(q))
				if err != nil {
					b.Fatal(err)
				}
				if len(res.Hits) == 0 {
					b.Fatalf("no hits for %q", q)
				}
			}
		})
	}
}
