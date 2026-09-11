package ga

import (
	"github.com/vcaesar/riot/analysis"
)

// this content was obtained from:
// lucene-4.7.2/analysis/common/src/resources/org/apache/lucene/analysis

var IrishArticles = []byte(`
d
m
b
`)

func Articles() analysis.TokenMap {
	rv := analysis.NewTokenMap()
	rv.LoadBytes(IrishArticles)
	return rv
}
