module github.com/blugelabs/bluge

go 1.16

require (
	github.com/RoaringBitmap/roaring v0.9.4
	github.com/axiomhq/hyperloglog v0.0.0-20191112132149-a4c4c47bc57f
	github.com/bits-and-blooms/bitset v1.2.0
	github.com/blevesearch/go-porterstemmer v1.0.3
	github.com/blevesearch/mmap-go v1.0.4
	github.com/blevesearch/segment v0.9.0
	github.com/blevesearch/snowballstem v0.9.0
	github.com/blevesearch/vellum v1.0.7
	github.com/blugelabs/bluge_segment_api v0.2.0
	github.com/blugelabs/ice v1.0.0
	github.com/caio/go-tdigest v3.1.0+incompatible
	github.com/leesper/go_rng v0.0.0-20190531154944-a612b043e353 // indirect
	github.com/spf13/cobra v0.0.5
	golang.org/x/sys v0.0.0-20220722155257-8c9f86f7a55f
	golang.org/x/text v0.3.8
	gonum.org/v1/gonum v0.7.0 // indirect
)

replace github.com/blugelabs/ice => github.com/zinclabs/ice v1.1.1

replace github.com/blugelabs/bluge_segment_api => github.com/zinclabs/bluge_segment_api v1.0.0
