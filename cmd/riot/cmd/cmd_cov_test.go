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

package cmd

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vcaesar/riot"
	"github.com/vcaesar/riot/index"
)

const exitHelperEnv = "RIOT_CMD_EXIT_HELPER"

// captureStdout runs f and returns everything it printed to os.Stdout.
func captureStdout(t *testing.T, f func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("error creating pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	runErr := f()
	os.Stdout = orig
	if err := w.Close(); err != nil {
		t.Fatalf("error closing pipe writer: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("error reading pipe: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("error closing pipe reader: %v", err)
	}
	return string(out), runErr
}

func runRoot(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var stderr bytes.Buffer
	RootCmd.SetArgs(args)
	RootCmd.SetOut(io.Discard)
	RootCmd.SetErr(&stderr)
	defer func() {
		RootCmd.SetArgs(nil)
		RootCmd.SetOut(nil)
		RootCmd.SetErr(nil)
	}()
	return captureStdout(t, RootCmd.Execute)
}

// buildIndex writes three docs, deletes one and returns the index path and
// the epochs of the persisted snapshots.
func buildIndex(t *testing.T) (path string, epochs []uint64) {
	t.Helper()
	path = t.TempDir()
	w, err := riot.OpenWriter(riot.DefaultConfig(path))
	if err != nil {
		t.Fatalf("error opening writer: %v", err)
	}
	batch := riot.NewBatch()
	for _, id := range []string{"a", "b", "c"} {
		batch.Insert(riot.NewDocument(id).AddField(riot.NewTextField("body", "hello "+id)))
	}
	if err := w.Batch(batch); err != nil {
		t.Fatalf("error indexing: %v", err)
	}
	if err := w.Delete(riot.Identifier("a")); err != nil {
		t.Fatalf("error deleting: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("error closing writer: %v", err)
	}
	epochs, err = index.NewFileSystemDirectory(path).List(index.ItemKindSnapshot)
	if err != nil {
		t.Fatalf("error listing snapshots: %v", err)
	}
	if len(epochs) == 0 {
		t.Fatal("expected at least one persisted snapshot")
	}
	return path, epochs
}

func writeSnapshotFile(t *testing.T, dir string, epoch uint64, data []byte) {
	t.Helper()
	name := filepath.Join(dir, fmt.Sprintf("%012x", epoch)+index.ItemKindSnapshot)
	if err := os.WriteFile(name, data, 0o600); err != nil {
		t.Fatalf("error writing snapshot file: %v", err)
	}
}

func TestListCommand(t *testing.T) {
	path, epochs := buildIndex(t)
	badDir := t.TempDir()
	writeSnapshotFile(t, badDir, 0, []byte("x"))
	if err := os.Rename(filepath.Join(badDir, fmt.Sprintf("%012x", 0)+index.ItemKindSnapshot),
		filepath.Join(badDir, "zz"+index.ItemKindSnapshot)); err != nil {
		t.Fatalf("error renaming: %v", err)
	}

	var want strings.Builder
	for _, epoch := range epochs {
		fmt.Fprintf(&want, "snapshot: %d\n", epoch)
	}

	tests := []struct {
		name    string
		args    []string
		want    string
		wantErr string
	}{
		{name: "missing path", args: []string{"list"}, wantErr: "must specify path to index"},
		{name: "missing dir", args: []string{"list", filepath.Join(path, "nope")}, wantErr: "error listing snapshots"},
		{name: "bad snapshot name", args: []string{"list", badDir}, wantErr: "error parsing identifier"},
		{name: "empty dir", args: []string{"list", t.TempDir()}},
		{name: "index", args: []string{"list", path}, want: want.String()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := runRoot(t, tt.args...)
			checkErr(t, err, tt.wantErr)
			if out != tt.want {
				t.Errorf("expected output %q, got %q", tt.want, out)
			}
		})
	}
}

func TestSnapshotCommand(t *testing.T) {
	path, epochs := buildIndex(t)
	epoch := fmt.Sprint(epochs[0])
	corrupt := t.TempDir()
	writeSnapshotFile(t, corrupt, 1, []byte{0x7f, 0x00})
	truncated := t.TempDir()
	writeSnapshotFile(t, truncated, 1, []byte{0x80})

	tests := []struct {
		name     string
		args     []string
		contains []string
		wantErr  string
	}{
		{name: "missing path", args: []string{"snapshot"}, wantErr: "must specify path to index"},
		{name: "missing epoch", args: []string{"snapshot", path}, wantErr: "must specify an epoch"},
		{name: "bad epoch", args: []string{"snapshot", path, "x"}, wantErr: "error parsing epoch as integer"},
		{name: "unknown epoch", args: []string{"snapshot", path, "999999"}, wantErr: "error loading snapshot from directory"},
		{name: "bad version", args: []string{"snapshot", corrupt, "1"}, wantErr: "error building snapshot from reader"},
		{name: "truncated", args: []string{"snapshot", truncated, "1"}, wantErr: "error building snapshot from reader"},
		{
			name:     "index",
			args:     []string{"snapshot", path, epoch},
			contains: []string{"snapshot: " + epoch + "\n", "segments:\n", "num_docs:        3", "num_deleted:    1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := runRoot(t, tt.args...)
			checkErr(t, err, tt.wantErr)
			for _, s := range tt.contains {
				if !strings.Contains(out, s) {
					t.Errorf("expected output to contain %q, got %q", s, out)
				}
			}
			if len(tt.contains) == 0 && out != "" {
				t.Errorf("expected no output, got %q", out)
			}
		})
	}
}

func checkErr(t *testing.T, err error, want string) {
	t.Helper()
	if want == "" {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return
	}
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("expected error containing %q, got %v", want, err)
	}
}

func TestExecuteSuccess(t *testing.T) {
	RootCmd.SetArgs([]string{"list", t.TempDir()})
	defer RootCmd.SetArgs(nil)
	out, err := captureStdout(t, func() error {
		Execute()
		return nil
	})
	if err != nil || out != "" {
		t.Fatalf("expected empty output, got %q, %v", out, err)
	}
}

// TestExecuteExitHelper is re-executed as a subprocess by TestExecuteExit.
func TestExecuteExitHelper(t *testing.T) {
	if os.Getenv(exitHelperEnv) != "1" {
		t.Skip("subprocess helper")
	}
	RootCmd.SetArgs([]string{"list"})
	RootCmd.SetErr(io.Discard)
	Execute()
}

func TestExecuteExit(t *testing.T) {
	args := []string{"-test.run=^TestExecuteExitHelper$"}
	// propagate the coverage dir so the subprocess counters get merged
	if f := flag.Lookup("test.gocoverdir"); f != nil && f.Value.String() != "" {
		args = append(args, "-test.gocoverdir="+f.Value.String())
	}
	c := exec.Command(os.Args[0], args...) //nolint:gosec // G204: re-exec of the test binary itself.
	c.Env = append(os.Environ(), exitHelperEnv+"=1")
	out, err := c.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() == 0 {
		t.Fatalf("expected non-zero exit, got %v, output %q", err, out)
	}
	if !strings.Contains(string(out), "must specify path to index") {
		t.Errorf("expected error message in output, got %q", out)
	}
}
