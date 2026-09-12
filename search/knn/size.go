// Copyright (c) 2026 The Bluge Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package knn

import (
	"reflect"

	"github.com/vcaesar/ice/vec"
)

func init() {
	var ptr *int
	sizeOfPtr = int(reflect.TypeOf(ptr).Size())
	var s Searcher
	reflectStaticSizeSearcher = int(reflect.TypeOf(s).Size())
	var m vec.Match
	reflectStaticSizeMatch = int(reflect.TypeOf(m).Size())
}

var sizeOfPtr int
var reflectStaticSizeSearcher int
var reflectStaticSizeMatch int
