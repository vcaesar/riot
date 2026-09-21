//  Copyright (c) 2026 The Riot Authors.
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

// Package gse provides a dictionary based tokenizer for Chinese, Japanese
// and other languages supported by github.com/go-ego/gse, plus a small
// gse-bleve style index API (New, Index, Search) built on riot.
package gse

import (
	"fmt"
	"strings"

	gogse "github.com/go-ego/gse"

	"github.com/vcaesar/riot/analysis"
)

const (
	embedPrefix = "embed, "
	modeSearch  = "search"
)

// Option configures the segmenter, the cut mode and the index.
type Option struct {
	// Index is the on-disk index path; empty opens an in-memory index.
	Index string
	// Field is the field for string documents and default searches; default "text".
	// Struct documents use their JSON field names instead.
	Field string
	// Lang selects a riot analysis/lang analyzer ("en", "cjk", "de", ...;
	// see Langs) instead of gse. When set, no gse dictionary is loaded, the
	// Segmenter is nil and Dicts/Stop/Opt/Alpha are ignored.
	Lang string
	// Dicts selects the dictionaries: "zh", "zh_s", "zh_t", "ja"/"jp" or
	// comma separated dictionary file paths. Prefix with "embed, " to use the
	// dictionaries compiled into gse instead of reading files from the gse
	// module directory. Empty loads the embedded "zh" dictionary.
	Dicts string
	// Stop selects the stop word dictionary: "zh", "embed, zh" or file paths.
	// Empty loads none. Stop words are dropped from the token stream.
	Stop string
	// Opt is the cut mode: "" (shortest path), "hmm", "dag", "search",
	// "search-hmm" or "search-dag". The search modes additionally emit the
	// sub-words of every segment and are meant for indexing; the Index
	// built by New queries with the matching non-search mode.
	Opt string
	// Alpha makes gse emit every Latin letter/digit as its own token.
	Alpha bool
}

// NewSegmenter loads the dictionaries described by opt.
func NewSegmenter(opt Option) (*gogse.Segmenter, error) {
	seg := &gogse.Segmenter{SkipLog: true, AlphaNum: opt.Alpha}
	var err error
	switch dicts := opt.Dicts; {
	case dicts == "":
		err = seg.LoadDictEmbed("zh")
	case strings.HasPrefix(dicts, embedPrefix):
		err = seg.LoadDictEmbed(strings.Replace(strings.TrimPrefix(dicts, embedPrefix), "jp", "ja", 1))
	default:
		err = seg.LoadDict(dicts)
	}
	if err != nil {
		return nil, fmt.Errorf("error loading gse dictionary %q: %v", opt.Dicts, err)
	}

	switch stop := opt.Stop; {
	case stop == "":
	case stop == embedPrefix+"zh":
		// gse only loads its embedded list via the no-argument form.
		err = seg.LoadStopEmbed()
	case strings.HasPrefix(stop, embedPrefix):
		err = seg.LoadStopEmbed(strings.TrimPrefix(stop, embedPrefix))
	default:
		err = seg.LoadStop(stop)
	}
	if err != nil {
		return nil, fmt.Errorf("error loading gse stop words %q: %v", opt.Stop, err)
	}
	return seg, nil
}

// NewAnalyzer builds an analyzer over an already loaded segmenter using opt.Opt
// as cut mode, so one segmenter can back both an index ("search-hmm") and a
// query ("hmm") analyzer. With a nil seg the analysis/lang analyzer named by
// opt.Lang is returned instead.
func NewAnalyzer(seg *gogse.Segmenter, opt Option) (*analysis.Analyzer, error) {
	if seg == nil {
		return NewLangAnalyzer(opt.Lang)
	}
	search, hmm, err := cutMode(opt.Opt)
	if err != nil {
		return nil, err
	}
	return &analysis.Analyzer{Tokenizer: NewTokenizer(seg, search, hmm...)}, nil
}

func cutMode(opt string) (search bool, hmm []bool, err error) {
	switch opt {
	case "":
	case "hmm":
		hmm = []bool{true}
	case "dag":
		hmm = []bool{false}
	case modeSearch:
		search = true
	case modeSearch + "-hmm":
		search, hmm = true, []bool{true}
	case modeSearch + "-dag":
		search, hmm = true, []bool{false}
	default:
		return false, nil, fmt.Errorf("error unknown gse cut mode %q", opt)
	}
	return search, hmm, nil
}
