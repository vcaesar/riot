// Copyright (c) 2026 The Bluge Authors.
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

import "testing"

func TestRootCommandRegistration(t *testing.T) {
	for _, name := range []string{"list", "snapshot"} {
		t.Run(name, func(t *testing.T) {
			command, args, err := RootCmd.Find([]string{name})
			if err != nil {
				t.Fatalf("error finding command: %v", err)
			}
			if command.Name() != name || len(args) != 0 {
				t.Fatalf("expected command %q, got %q with args %v", name, command.Name(), args)
			}
			if command.Parent() != RootCmd {
				t.Fatal("command is not attached to RootCmd")
			}
			if command.RunE == nil {
				t.Fatal("command has no RunE handler")
			}
			if err := command.RunE(command, nil); err == nil || err.Error() != "must specify path to index" {
				t.Fatalf("expected missing path error, got %v", err)
			}
		})
	}
}
