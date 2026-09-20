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

import "strings"

const defaultAnsiHighlight = BgYellow

type ANSIFragmentFormatter struct {
	color string
}

func NewANSIFragmentFormatter() *ANSIFragmentFormatter {
	return NewANSIFragmentFormatterColor(defaultAnsiHighlight)
}

func NewANSIFragmentFormatterColor(color string) *ANSIFragmentFormatter {
	return &ANSIFragmentFormatter{
		color: color,
	}
}

func (a *ANSIFragmentFormatter) Format(f *Fragment, orderedTermLocations TermLocations) string {
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
		rv.Write(f.Orig[curr:termLocation.Start])
		// add the color
		rv.WriteString(a.color)
		// add the term itself
		rv.Write(f.Orig[termLocation.Start:termLocation.End])
		// reset the color
		rv.WriteString(Reset)
		// update current
		curr = termLocation.End
	}
	// add any remaining text after the last token
	rv.Write(f.Orig[curr:f.End])

	return rv.String()
}

// ANSI color control escape sequences.
// Shamelessly copied from https://github.com/sqp/godock/blob/master/libs/log/colors.go
const (
	Reset      = "\x1b[0m"
	Bright     = "\x1b[1m"
	Dim        = "\x1b[2m"
	Underscore = "\x1b[4m"
	Blink      = "\x1b[5m"
	Reverse    = "\x1b[7m"
	Hidden     = "\x1b[8m"
	FgBlack    = "\x1b[30m"
	FgRed      = "\x1b[31m"
	FgGreen    = "\x1b[32m"
	FgYellow   = "\x1b[33m"
	FgBlue     = "\x1b[34m"
	FgMagenta  = "\x1b[35m"
	FgCyan     = "\x1b[36m"
	FgWhite    = "\x1b[37m"
	BgBlack    = "\x1b[40m"
	BgRed      = "\x1b[41m"
	BgGreen    = "\x1b[42m"
	BgYellow   = "\x1b[43m"
	BgBlue     = "\x1b[44m"
	BgMagenta  = "\x1b[45m"
	BgCyan     = "\x1b[46m"
	BgWhite    = "\x1b[47m"
)
