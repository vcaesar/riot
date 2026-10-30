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

package index

import (
	"testing"
	"time"
)

// TestPersisterNotifiesStaleMergerWatcher verifies a merger watcher registered
// after its epoch was already persisted is released without waiting for
// another introduction.
func TestPersisterNotifiesStaleMergerWatcher(t *testing.T) {
	s := &Writer{closeCh: make(chan struct{})}
	introducerNotifier := make(watcherChan, 1)
	persisterNotifier := make(watcherChan, 1)

	const lastPersistedEpoch = 5
	s.asyncTasks.Add(1)
	go s.persisterLoop(nil, nil, introducerNotifier, persisterNotifier, lastPersistedEpoch)
	defer func() {
		close(s.closeCh)
		s.asyncTasks.Wait()
	}()

	ew, err := persisterNotifier.NotifyUsAfter(lastPersistedEpoch-2, s.closeCh)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-ew.notifyCh:
	case <-time.After(5 * time.Second):
		t.Fatal("watcher for an already persisted epoch was never notified")
	}
}
