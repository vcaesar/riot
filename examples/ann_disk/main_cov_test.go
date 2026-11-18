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

package main

import (
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	riot "github.com/vcaesar/riot"
)

const mainHelperEnv = "RIOT_ANN_DISK_MAIN_HELPER"

var errWrite = errors.New("write failed")

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errWrite }

func setTempDirEnv(t *testing.T, dir string) {
	t.Helper()
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(key, dir)
	}
}

func TestMainPrints(t *testing.T) {
	tmp := t.TempDir()
	setTempDirEnv(t, tmp)
	outPath := filepath.Join(t.TempDir(), "stdout")
	f, err := os.Create(outPath)
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = f
	main()
	os.Stdout = orig
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected temp index removed, found %d entries", len(entries))
	}
}

// TestMainHelper is re-executed as a subprocess by TestMainFatal.
func TestMainHelper(t *testing.T) {
	if os.Getenv(mainHelperEnv) != "1" {
		t.Skip("subprocess helper")
	}
	main()
}

func TestMainFatal(t *testing.T) {
	readOnly := filepath.Join(t.TempDir(), "stdout")
	if err := os.WriteFile(readOnly, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		tmp    string
		stdout string
	}{
		{name: "mkdir temp fails", tmp: filepath.Join(t.TempDir(), "missing")},
		{name: "stdout write fails", tmp: t.TempDir(), stdout: readOnly},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := []string{"-test.run=^TestMainHelper$"}
			// propagate the coverage dir so the subprocess counters get merged
			if f := flag.Lookup("test.gocoverdir"); f != nil && f.Value.String() != "" {
				args = append(args, "-test.gocoverdir="+f.Value.String())
			}
			c := exec.Command(os.Args[0], args...) //nolint:gosec // G204: re-exec of the test binary itself.
			c.Env = append(os.Environ(), mainHelperEnv+"=1", "TMPDIR="+tt.tmp, "TMP="+tt.tmp, "TEMP="+tt.tmp)
			if tt.stdout != "" {
				f, err := os.Open(tt.stdout)
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := f.Close(); err != nil {
						t.Error(err)
					}
				}()
				c.Stdout = f
			}
			err := c.Run()
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() == 0 {
				t.Fatalf("expected non-zero exit, got %v", err)
			}
		})
	}
}

func TestRunErrors(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(failWriter{}, file); err == nil {
		t.Fatal("expected error opening index on a regular file")
	}
}

func TestSearchErrors(t *testing.T) {
	indexed := riot.DefaultConfig(t.TempDir())
	if err := index(indexed); err != nil {
		t.Fatal(err)
	}

	mismatched := riot.DefaultConfig(t.TempDir())
	writer, err := riot.OpenWriter(mismatched)
	if err != nil {
		t.Fatal(err)
	}
	field, err := riot.NewVectorField("embedding", []float32{1, 0, 0})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Insert(riot.NewDocument("x").AddField(field)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		config riot.Config
		out    failWriter
		want   error
	}{
		{name: "missing index", config: riot.DefaultConfig(filepath.Join(t.TempDir(), "missing"))},
		{name: "dimension mismatch", config: mismatched},
		{name: "write fails", config: indexed, want: errWrite},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := search(tt.out, tt.config)
			if err == nil {
				t.Fatal("expected error")
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, err)
			}
		})
	}
}
