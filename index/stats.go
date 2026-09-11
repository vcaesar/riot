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

package index

import (
	"sync/atomic"
)

// Stats returns a race-safe snapshot. Fields are sampled independently, not
// as a single point-in-time view of the index.
func (s *Writer) Stats() Stats {
	rv := Stats{
		TotUpdates:                         atomic.LoadUint64(&s.stats.TotUpdates),
		TotDeletes:                         atomic.LoadUint64(&s.stats.TotDeletes),
		TotBatches:                         atomic.LoadUint64(&s.stats.TotBatches),
		TotBatchesEmpty:                    atomic.LoadUint64(&s.stats.TotBatchesEmpty),
		TotBatchIntroTime:                  atomic.LoadUint64(&s.stats.TotBatchIntroTime),
		MaxBatchIntroTime:                  atomic.LoadUint64(&s.stats.MaxBatchIntroTime),
		CurRootEpoch:                       atomic.LoadUint64(&s.stats.CurRootEpoch),
		LastPersistedEpoch:                 atomic.LoadUint64(&s.stats.LastPersistedEpoch),
		LastMergedEpoch:                    atomic.LoadUint64(&s.stats.LastMergedEpoch),
		TotOnErrors:                        atomic.LoadUint64(&s.stats.TotOnErrors),
		TotAnalysisTime:                    atomic.LoadUint64(&s.stats.TotAnalysisTime),
		TotIndexTime:                       atomic.LoadUint64(&s.stats.TotIndexTime),
		TotIndexedPlainTextBytes:           atomic.LoadUint64(&s.stats.TotIndexedPlainTextBytes),
		TotTermSearchersStarted:            atomic.LoadUint64(&s.stats.TotTermSearchersStarted),
		TotTermSearchersFinished:           atomic.LoadUint64(&s.stats.TotTermSearchersFinished),
		TotIntroduceLoop:                   atomic.LoadUint64(&s.stats.TotIntroduceLoop),
		TotIntroduceSegmentBeg:             atomic.LoadUint64(&s.stats.TotIntroduceSegmentBeg),
		TotIntroduceSegmentEnd:             atomic.LoadUint64(&s.stats.TotIntroduceSegmentEnd),
		TotIntroducePersistBeg:             atomic.LoadUint64(&s.stats.TotIntroducePersistBeg),
		TotIntroducePersistEnd:             atomic.LoadUint64(&s.stats.TotIntroducePersistEnd),
		TotIntroduceMergeBeg:               atomic.LoadUint64(&s.stats.TotIntroduceMergeBeg),
		TotIntroduceMergeEnd:               atomic.LoadUint64(&s.stats.TotIntroduceMergeEnd),
		TotIntroduceRevertBeg:              atomic.LoadUint64(&s.stats.TotIntroduceRevertBeg),
		TotIntroduceRevertEnd:              atomic.LoadUint64(&s.stats.TotIntroduceRevertEnd),
		TotIntroducedItems:                 atomic.LoadUint64(&s.stats.TotIntroducedItems),
		TotIntroducedSegmentsBatch:         atomic.LoadUint64(&s.stats.TotIntroducedSegmentsBatch),
		TotIntroducedSegmentsMerge:         atomic.LoadUint64(&s.stats.TotIntroducedSegmentsMerge),
		TotPersistLoopBeg:                  atomic.LoadUint64(&s.stats.TotPersistLoopBeg),
		TotPersistLoopErr:                  atomic.LoadUint64(&s.stats.TotPersistLoopErr),
		TotPersistLoopProgress:             atomic.LoadUint64(&s.stats.TotPersistLoopProgress),
		TotPersistLoopWait:                 atomic.LoadUint64(&s.stats.TotPersistLoopWait),
		TotPersistLoopWaitNotified:         atomic.LoadUint64(&s.stats.TotPersistLoopWaitNotified),
		TotPersistLoopEnd:                  atomic.LoadUint64(&s.stats.TotPersistLoopEnd),
		TotPersistedItems:                  atomic.LoadUint64(&s.stats.TotPersistedItems),
		TotItemsToPersist:                  atomic.LoadUint64(&s.stats.TotItemsToPersist),
		TotPersistedSegments:               atomic.LoadUint64(&s.stats.TotPersistedSegments),
		TotPersisterSlowMergerPause:        atomic.LoadUint64(&s.stats.TotPersisterSlowMergerPause),
		TotPersisterSlowMergerResume:       atomic.LoadUint64(&s.stats.TotPersisterSlowMergerResume),
		TotPersisterNapPauseCompleted:      atomic.LoadUint64(&s.stats.TotPersisterNapPauseCompleted),
		TotPersisterMergerNapBreak:         atomic.LoadUint64(&s.stats.TotPersisterMergerNapBreak),
		TotFileMergeLoopBeg:                atomic.LoadUint64(&s.stats.TotFileMergeLoopBeg),
		TotFileMergeLoopErr:                atomic.LoadUint64(&s.stats.TotFileMergeLoopErr),
		TotFileMergeLoopEnd:                atomic.LoadUint64(&s.stats.TotFileMergeLoopEnd),
		TotFileMergePlan:                   atomic.LoadUint64(&s.stats.TotFileMergePlan),
		TotFileMergePlanErr:                atomic.LoadUint64(&s.stats.TotFileMergePlanErr),
		TotFileMergePlanNone:               atomic.LoadUint64(&s.stats.TotFileMergePlanNone),
		TotFileMergePlanOk:                 atomic.LoadUint64(&s.stats.TotFileMergePlanOk),
		TotFileMergePlanTasks:              atomic.LoadUint64(&s.stats.TotFileMergePlanTasks),
		TotFileMergePlanTasksDone:          atomic.LoadUint64(&s.stats.TotFileMergePlanTasksDone),
		TotFileMergePlanTasksErr:           atomic.LoadUint64(&s.stats.TotFileMergePlanTasksErr),
		TotFileMergePlanTasksSegments:      atomic.LoadUint64(&s.stats.TotFileMergePlanTasksSegments),
		TotFileMergePlanTasksSegmentsEmpty: atomic.LoadUint64(&s.stats.TotFileMergePlanTasksSegmentsEmpty),
		TotFileMergeSegmentsEmpty:          atomic.LoadUint64(&s.stats.TotFileMergeSegmentsEmpty),
		TotFileMergeSegments:               atomic.LoadUint64(&s.stats.TotFileMergeSegments),
		TotFileSegmentsAtRoot:              atomic.LoadUint64(&s.stats.TotFileSegmentsAtRoot),
		TotFileMergeWrittenBytes:           atomic.LoadUint64(&s.stats.TotFileMergeWrittenBytes),
		TotFileMergeZapBeg:                 atomic.LoadUint64(&s.stats.TotFileMergeZapBeg),
		TotFileMergeZapEnd:                 atomic.LoadUint64(&s.stats.TotFileMergeZapEnd),
		TotFileMergeZapTime:                atomic.LoadUint64(&s.stats.TotFileMergeZapTime),
		MaxFileMergeZapTime:                atomic.LoadUint64(&s.stats.MaxFileMergeZapTime),
		TotFileMergeZapIntroductionTime:    atomic.LoadUint64(&s.stats.TotFileMergeZapIntroductionTime),
		MaxFileMergeZapIntroductionTime:    atomic.LoadUint64(&s.stats.MaxFileMergeZapIntroductionTime),
		TotFileMergeIntroductions:          atomic.LoadUint64(&s.stats.TotFileMergeIntroductions),
		TotFileMergeIntroductionsDone:      atomic.LoadUint64(&s.stats.TotFileMergeIntroductionsDone),
		TotFileMergeIntroductionsSkipped:   atomic.LoadUint64(&s.stats.TotFileMergeIntroductionsSkipped),
		TotFileMergeIntroductionsObsoleted: atomic.LoadUint64(&s.stats.TotFileMergeIntroductionsObsoleted),
		CurFilesIneligibleForRemoval:       atomic.LoadUint64(&s.stats.CurFilesIneligibleForRemoval),
		TotSnapshotsRemovedFromMetaStore:   atomic.LoadUint64(&s.stats.TotSnapshotsRemovedFromMetaStore),
		TotMemMergeBeg:                     atomic.LoadUint64(&s.stats.TotMemMergeBeg),
		TotMemMergeErr:                     atomic.LoadUint64(&s.stats.TotMemMergeErr),
		TotMemMergeDone:                    atomic.LoadUint64(&s.stats.TotMemMergeDone),
		TotMemMergeZapBeg:                  atomic.LoadUint64(&s.stats.TotMemMergeZapBeg),
		TotMemMergeZapEnd:                  atomic.LoadUint64(&s.stats.TotMemMergeZapEnd),
		TotMemMergeZapTime:                 atomic.LoadUint64(&s.stats.TotMemMergeZapTime),
		MaxMemMergeZapTime:                 atomic.LoadUint64(&s.stats.MaxMemMergeZapTime),
		TotMemMergeSegments:                atomic.LoadUint64(&s.stats.TotMemMergeSegments),
		TotMemorySegmentsAtRoot:            atomic.LoadUint64(&s.stats.TotMemorySegmentsAtRoot),
		TotEventFired:                      atomic.LoadUint64(&s.stats.TotEventFired),
		TotEventReturned:                   atomic.LoadUint64(&s.stats.TotEventReturned),
		CurOnDiskBytesUsedByRoot:           atomic.LoadUint64(&s.stats.CurOnDiskBytesUsedByRoot),
		persistEpoch:                       atomic.LoadUint64(&s.stats.persistEpoch),
		persistSnapshotSize:                atomic.LoadUint64(&s.stats.persistSnapshotSize),
		mergeEpoch:                         atomic.LoadUint64(&s.stats.mergeEpoch),
		mergeSnapshotSize:                  atomic.LoadUint64(&s.stats.mergeSnapshotSize),
		newSegBufBytesAdded:                atomic.LoadUint64(&s.stats.newSegBufBytesAdded),
		newSegBufBytesRemoved:              atomic.LoadUint64(&s.stats.newSegBufBytesRemoved),
		analysisBytesAdded:                 atomic.LoadUint64(&s.stats.analysisBytesAdded),
		analysisBytesRemoved:               atomic.LoadUint64(&s.stats.analysisBytesRemoved),
	}
	rv.CurOnDiskFiles, rv.CurOnDiskBytes = s.DirectoryStats()
	return rv
}

// DirectoryStats returns the directory's total item count and cumulative size in bytes.
func (s *Writer) DirectoryStats() (numItems, numBytes uint64) {
	return s.directory.Stats()
}

// Stats tracks statistics about the index, fields that are
// prefixed like CurXxxx are gauges (can go up and down),
// and fields that are prefixed like TotXxxx are monotonically
// increasing counters.
type Stats struct {
	TotUpdates uint64
	TotDeletes uint64

	TotBatches        uint64
	TotBatchesEmpty   uint64
	TotBatchIntroTime uint64
	MaxBatchIntroTime uint64

	CurRootEpoch       uint64
	LastPersistedEpoch uint64
	LastMergedEpoch    uint64

	TotOnErrors uint64

	TotAnalysisTime uint64
	TotIndexTime    uint64

	TotIndexedPlainTextBytes uint64

	TotTermSearchersStarted  uint64
	TotTermSearchersFinished uint64

	TotIntroduceLoop       uint64
	TotIntroduceSegmentBeg uint64
	TotIntroduceSegmentEnd uint64
	TotIntroducePersistBeg uint64
	TotIntroducePersistEnd uint64
	TotIntroduceMergeBeg   uint64
	TotIntroduceMergeEnd   uint64
	TotIntroduceRevertBeg  uint64
	TotIntroduceRevertEnd  uint64

	TotIntroducedItems         uint64
	TotIntroducedSegmentsBatch uint64
	TotIntroducedSegmentsMerge uint64

	TotPersistLoopBeg          uint64
	TotPersistLoopErr          uint64
	TotPersistLoopProgress     uint64
	TotPersistLoopWait         uint64
	TotPersistLoopWaitNotified uint64
	TotPersistLoopEnd          uint64

	TotPersistedItems    uint64
	TotItemsToPersist    uint64
	TotPersistedSegments uint64

	TotPersisterSlowMergerPause  uint64
	TotPersisterSlowMergerResume uint64

	TotPersisterNapPauseCompleted uint64
	TotPersisterMergerNapBreak    uint64

	TotFileMergeLoopBeg uint64
	TotFileMergeLoopErr uint64
	TotFileMergeLoopEnd uint64

	TotFileMergePlan     uint64
	TotFileMergePlanErr  uint64
	TotFileMergePlanNone uint64
	TotFileMergePlanOk   uint64

	TotFileMergePlanTasks              uint64
	TotFileMergePlanTasksDone          uint64
	TotFileMergePlanTasksErr           uint64
	TotFileMergePlanTasksSegments      uint64
	TotFileMergePlanTasksSegmentsEmpty uint64

	TotFileMergeSegmentsEmpty uint64
	TotFileMergeSegments      uint64
	TotFileSegmentsAtRoot     uint64
	TotFileMergeWrittenBytes  uint64

	TotFileMergeZapBeg              uint64
	TotFileMergeZapEnd              uint64
	TotFileMergeZapTime             uint64
	MaxFileMergeZapTime             uint64
	TotFileMergeZapIntroductionTime uint64
	MaxFileMergeZapIntroductionTime uint64

	TotFileMergeIntroductions          uint64
	TotFileMergeIntroductionsDone      uint64
	TotFileMergeIntroductionsSkipped   uint64
	TotFileMergeIntroductionsObsoleted uint64

	CurFilesIneligibleForRemoval     uint64
	TotSnapshotsRemovedFromMetaStore uint64

	TotMemMergeBeg          uint64
	TotMemMergeErr          uint64
	TotMemMergeDone         uint64
	TotMemMergeZapBeg       uint64
	TotMemMergeZapEnd       uint64
	TotMemMergeZapTime      uint64
	MaxMemMergeZapTime      uint64
	TotMemMergeSegments     uint64
	TotMemorySegmentsAtRoot uint64

	TotEventFired    uint64
	TotEventReturned uint64

	CurOnDiskBytes           uint64
	CurOnDiskBytesUsedByRoot uint64 // FIXME not currently supported
	CurOnDiskFiles           uint64

	// the following stats are only used internally
	persistEpoch          uint64
	persistSnapshotSize   uint64
	mergeEpoch            uint64
	mergeSnapshotSize     uint64
	newSegBufBytesAdded   uint64
	newSegBufBytesRemoved uint64
	analysisBytesAdded    uint64
	analysisBytesRemoved  uint64
}

func (s *Writer) numEventsBlocking() int {
	eventsReturned := atomic.LoadUint64(&s.stats.TotEventReturned)
	eventsFired := atomic.LoadUint64(&s.stats.TotEventFired)
	return int(eventsFired - eventsReturned)
}
