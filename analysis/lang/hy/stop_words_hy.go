package hy

import (
	"github.com/vcaesar/riot/analysis"
)

// this content was obtained from:
// lucene-4.7.2/analysis/common/src/resources/org/apache/lucene/analysis/
// ` was changed to ' to allow for literal string

var StopWordsBytes = []byte(`# example set of Armenian stopwords.
այդ
այլ
այն
այս
դու
դուք
եմ
են
ենք
ես
եք
է
էի
էին
էինք
էիր
էիք
էր
ըստ
թ
ի
ին
իսկ
իր
կամ
համար
հետ
հետո
մենք
մեջ
մի
ն
նա
նաև
նրա
նրանք
որ
որը
որոնք
որպես
ու
ում
պիտի
վրա
և
`)

func StopWords() analysis.TokenMap {
	rv := analysis.NewTokenMap()
	rv.LoadBytes(StopWordsBytes)
	return rv
}
