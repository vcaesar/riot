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

// Package lang is the registry of language analyzers selected by code, for
// example riot.Config.WithLang("de").
//
// Only "standard", "simple", "keyword", "web" and "en" are built in. A
// language package registers its code when imported, so a binary links only
// the languages it activates:
//
//	import _ "github.com/vcaesar/riot/analysis/lang/de"
package lang

import (
	"fmt"
	"sort"
	"sync"

	"github.com/vcaesar/riot/analysis"
	"github.com/vcaesar/riot/analysis/analyzer"
	"github.com/vcaesar/riot/analysis/lang/en"
)

var (
	mu        sync.RWMutex
	analyzers = map[string]func() *analysis.Analyzer{
		"standard": analyzer.NewStandardAnalyzer,
		"simple":   analyzer.NewSimpleAnalyzer,
		"keyword":  analyzer.NewKeywordAnalyzer,
		"web":      analyzer.NewWebAnalyzer,
		"en":       en.NewAnalyzer,
	}
)

// Register makes the analyzer constructor available under code; a later
// registration of the same code replaces it.
func Register(code string, newAnalyzer func() *analysis.Analyzer) {
	mu.Lock()
	analyzers[code] = newAnalyzer
	mu.Unlock()
}

// Codes lists the registered codes, sorted.
func Codes() []string {
	mu.RLock()
	codes := make([]string, 0, len(analyzers))
	for code := range analyzers {
		codes = append(codes, code)
	}
	mu.RUnlock()
	sort.Strings(codes)
	return codes
}

// New returns a new analyzer for code.
func New(code string) (*analysis.Analyzer, error) {
	mu.RLock()
	newAnalyzer, ok := analyzers[code]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("error unknown language analyzer %q (import its analysis/lang package to activate it), want one of %v",
			code, Codes())
	}
	return newAnalyzer(), nil
}
