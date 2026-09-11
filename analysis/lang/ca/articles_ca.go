package ca

import (
	"github.com/vcaesar/riot/analysis"
)

const ArticlesName = "articles_ca"

// this content was obtained from:
// lucene-4.7.2/analysis/common/src/resources/org/apache/lucene/analysis

var CatalanArticles = []byte(`
d
l
m
n
s
t
`)

func Articles() analysis.TokenMap {
	rv := analysis.NewTokenMap()
	rv.LoadBytes(CatalanArticles)
	return rv
}
