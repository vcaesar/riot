package fr

import (
	"github.com/vcaesar/riot/analysis"
)

// this content was obtained from:
// lucene-4.7.2/analysis/common/src/resources/org/apache/lucene/analysis

var FrenchArticles = []byte(`
l
m
t
qu
n
s
j
d
c
jusqu
quoiqu
lorsqu
puisqu
`)

func Articles() analysis.TokenMap {
	rv := analysis.NewTokenMap()
	rv.LoadBytes(FrenchArticles)
	return rv
}
