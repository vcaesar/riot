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

package riot

import "github.com/vcaesar/riot/index"

// Stream indexes documents with bounded memory: each document is analyzed
// and packed as it is added, and full buffers become segments that Commit
// publishes atomically. See index.Stream.
type Stream = index.Stream

// NewStream returns a Stream that builds a segment every memoryBudget bytes
// of packed documents; non-positive uses index.DefaultStreamMemoryBudget.
func (w *Writer) NewStream(memoryBudget int) *Stream {
	return w.chill.NewStream(memoryBudget)
}
