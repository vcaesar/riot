//  Copyright (c) 2020 Couchbase, Inc.
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

package highlight

import (
	"html"
	"strings"
)

const defaultHTMLHighlightBefore = "<mark>"
const defaultHTMLHighlightAfter = "</mark>"

type HTMLFragmentFormatter struct {
	before string
	after  string
}

func NewHTMLFragmentFormatter() *HTMLFragmentFormatter {
	return NewHTMLFragmentFormatterTags(defaultHTMLHighlightBefore, defaultHTMLHighlightAfter)
}

func NewHTMLFragmentFormatterTags(before, after string) *HTMLFragmentFormatter {
	return &HTMLFragmentFormatter{
		before: before,
		after:  after,
	}
}

func (a *HTMLFragmentFormatter) Format(f *Fragment, orderedTermLocations TermLocations) string {
	if len(orderedTermLocations) == 0 {
		return html.EscapeString(string(f.Orig[f.Start:f.End]))
	}
	var rv strings.Builder
	curr := f.Start
	for _, termLocation := range orderedTermLocations {
		if termLocation == nil {
			continue
		}
		if termLocation.Start < curr {
			continue
		}
		if termLocation.End > f.End {
			break
		}
		// add the stuff before this location
		rv.WriteString(html.EscapeString(string(f.Orig[curr:termLocation.Start])))
		// start the <mark> tag
		rv.WriteString(a.before)
		// add the term itself
		rv.WriteString(html.EscapeString(string(f.Orig[termLocation.Start:termLocation.End])))
		// end the <mark> tag
		rv.WriteString(a.after)
		// update current
		curr = termLocation.End
	}
	// add any remaining text after the last token
	rv.WriteString(html.EscapeString(string(f.Orig[curr:f.End])))

	return rv.String()
}
