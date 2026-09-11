package ga

import (
	"github.com/vcaesar/riot/analysis"
)

// this content was obtained from:
// lucene-4.7.2/analysis/common/src/resources/org/apache/lucene/analysis/snowball/
// ` was changed to ' to allow for literal string

var StopWordsBytes = []byte(`
a
ach
ag
agus
an
aon
ar
arna
as
b'
ba
beirt
bhúr
caoga
ceathair
ceathrar
chomh
chtó
chuig
chun
cois
céad
cúig
cúigear
d'
daichead
dar
de
deich
deichniúr
den
dhá
do
don
dtí
dá
dár
dó
faoi
faoin
faoina
faoinár
fara
fiche
gach
gan
go
gur
haon
hocht
i
iad
idir
in
ina
ins
inár
is
le
leis
lena
lenár
m'
mar
mo
mé
na
nach
naoi
naonúr
ná
ní
níor
nó
nócha
ocht
ochtar
os
roimh
sa
seacht
seachtar
seachtó
seasca
seisear
siad
sibh
sinn
sna
sé
sí
tar
thar
thú
triúr
trí
trína
trínár
tríocha
tú
um
ár
é
éis
í
ó
ón
óna
ónár
`)

func StopWords() analysis.TokenMap {
	rv := analysis.NewTokenMap()
	rv.LoadBytes(StopWordsBytes)
	return rv
}
