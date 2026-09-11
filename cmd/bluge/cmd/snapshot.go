//  Copyright (c) 2020 The Bluge Authors.
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

package cmd

import (
	"fmt"
	"strconv"

	"github.com/vcaesar/riot/index"

	"github.com/spf13/cobra"
)

const snapshotArgEpoch = 2

var snapshotCmd = &cobra.Command{
	Use:   "snapshot [path] [epoch]",
	Short: "snapshot prints the details of the snapshot with the specified epoch",
	Long:  `The snapshot command will print the details of the snapshot with the specified epoch.`,
	RunE: func(_ *cobra.Command, args []string) error {

		if len(args) < 1 {
			return fmt.Errorf("must specify path to index")
		}

		if len(args) < snapshotArgEpoch {
			return fmt.Errorf("must specify an epoch")
		}

		dir := index.NewFileSystemDirectory(args[0])

		epoch, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("error parsing epoch as integer: %v", err)
		}

		data, closer, err := dir.Load(index.ItemKindSnapshot, uint64(epoch))
		if err != nil {
			return fmt.Errorf("error loading snapshot from directory: %v", err)
		}
		defer func() {
			_ = closer.Close()
		}()

		var snapshot index.Snapshot
		_, err = snapshot.ReadFrom(data.Reader())
		if err != nil {
			return fmt.Errorf("error building snapshot from reader: %v", err)
		}

		fmt.Printf("snapshot: %d\n", epoch)
		fmt.Printf("segments:\n")
		for _, segSnapshot := range snapshot.Segments() {
			var numDeleted int
			if segSnapshot.Deleted() != nil {
				numDeleted = int(segSnapshot.Deleted().GetCardinality())
			}
			docTimeMin, docTimeMax := segSnapshot.Timestamp()
			fmt.Printf("segment id: %5d num_docs: %8d, num_deleted: %4d, doc_time_min: %d, doc_time_max: %d, segment_size: %d\n",
				segSnapshot.ID(), segSnapshot.DocNum(), numDeleted, docTimeMin, docTimeMax, segSnapshot.SegmentSize())
		}

		return nil
	},
}

func init() {
	RootCmd.AddCommand(snapshotCmd)
}
