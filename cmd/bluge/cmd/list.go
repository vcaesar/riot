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

	"github.com/vcaesar/riot/index"

	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list [path]",
	Short: "lists the contents of the bluge index",
	Long:  `The list command will list the contents of the Bluge index.`,
	RunE: func(cmd *cobra.Command, args []string) error {

		if len(args) < 1 {
			return fmt.Errorf("must specify path to index")
		}

		dir := index.NewFileSystemDirectory(args[0])

		snapshotIDs, err := dir.List(index.ItemKindSnapshot)
		if err != nil {
			return fmt.Errorf("error listing snapshots: %v", err)
		}
		for _, snapshotID := range snapshotIDs {
			fmt.Printf("snapshot: %d\n", snapshotID)
		}

		return nil
	},
}

func init() {
	RootCmd.AddCommand(listCmd)
}
