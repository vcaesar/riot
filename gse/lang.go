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

package gse

import (
	"fmt"
	"sort"

	"github.com/vcaesar/riot/analysis"
	"github.com/vcaesar/riot/analysis/analyzer"
	"github.com/vcaesar/riot/analysis/lang/ar"
	"github.com/vcaesar/riot/analysis/lang/cjk"
	"github.com/vcaesar/riot/analysis/lang/ckb"
	"github.com/vcaesar/riot/analysis/lang/da"
	"github.com/vcaesar/riot/analysis/lang/de"
	"github.com/vcaesar/riot/analysis/lang/en"
	"github.com/vcaesar/riot/analysis/lang/es"
	"github.com/vcaesar/riot/analysis/lang/fa"
	"github.com/vcaesar/riot/analysis/lang/fi"
	"github.com/vcaesar/riot/analysis/lang/fr"
	"github.com/vcaesar/riot/analysis/lang/hi"
	"github.com/vcaesar/riot/analysis/lang/hu"
	"github.com/vcaesar/riot/analysis/lang/it"
	"github.com/vcaesar/riot/analysis/lang/nl"
	"github.com/vcaesar/riot/analysis/lang/no"
	"github.com/vcaesar/riot/analysis/lang/pt"
	"github.com/vcaesar/riot/analysis/lang/ro"
	"github.com/vcaesar/riot/analysis/lang/ru"
	"github.com/vcaesar/riot/analysis/lang/sv"
	"github.com/vcaesar/riot/analysis/lang/tr"
)

// langAnalyzers maps Option.Lang codes to the riot analyzers that do not
// need a gse segmenter: the analysis/lang packages plus the generic
// "standard", "simple", "keyword" and "web" analyzers.
var langAnalyzers = map[string]func() *analysis.Analyzer{
	"standard": analyzer.NewStandardAnalyzer,
	"simple":   analyzer.NewSimpleAnalyzer,
	"keyword":  analyzer.NewKeywordAnalyzer,
	"web":      analyzer.NewWebAnalyzer,
	"ar":       ar.Analyzer,
	"cjk":      cjk.Analyzer,
	"ckb":      ckb.Analyzer,
	"da":       da.Analyzer,
	"de":       de.Analyzer,
	"en":       en.NewAnalyzer,
	"es":       es.Analyzer,
	"fa":       fa.Analyzer,
	"fi":       fi.Analyzer,
	"fr":       fr.Analyzer,
	"hi":       hi.Analyzer,
	"hu":       hu.Analyzer,
	"it":       it.Analyzer,
	"nl":       nl.Analyzer,
	"no":       no.Analyzer,
	"pt":       pt.Analyzer,
	"ro":       ro.Analyzer,
	"ru":       ru.Analyzer,
	"sv":       sv.Analyzer,
	"tr":       tr.Analyzer,
}

// Langs lists the codes accepted by Option.Lang.
func Langs() []string {
	codes := make([]string, 0, len(langAnalyzers))
	for code := range langAnalyzers {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

// NewLangAnalyzer returns the analysis/lang analyzer for code.
func NewLangAnalyzer(code string) (*analysis.Analyzer, error) {
	newAnalyzer, ok := langAnalyzers[code]
	if !ok {
		return nil, fmt.Errorf("error unknown language analyzer %q, want one of %v", code, Langs())
	}
	return newAnalyzer(), nil
}
