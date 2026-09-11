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

package lock

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const pathEnv = "TESTPATH"
const modeEnv = "MODE"
const modeExclusive = "EXCLUSIVE"
const expectEnv = "EXPECT"
const expectError = "ERROR"
const testBoot = time.Second
const lockHold = 5 * time.Second

// TestSubProcessHelper is invoked by sub-processes to attempt getting
// exclusive/shared locks on the specified path, and either expect
// an error or not.  All expectations are set by environment variables.
func TestSubProcessHelper(t *testing.T) {
	testPath := os.Getenv(pathEnv)
	if testPath != "" {
		var err error
		var f LockedFile
		if os.Getenv(modeEnv) == modeExclusive {
			f, err = OpenExclusive(testPath, os.O_CREATE|os.O_RDWR, os.FileMode(0600))
		} else {
			f, err = OpenShared(testPath, os.O_RDONLY, 0)
		}
		if f != nil {
			defer func() {
				err = f.Close()
				if err != nil {
					t.Fatalf("error closing file: %v", err)
				}
			}()
		}
		if os.Getenv(expectEnv) == expectError && err == nil {
			t.Fatalf("other process, path '%s' mode %s expected error, got nil", testPath, os.Getenv(modeEnv))
		} else if os.Getenv(expectEnv) != expectError && err != nil {
			t.Fatalf("other process, path '%s' mode %s expected no error, got %v", testPath, os.Getenv(modeEnv), err)
		}
		time.Sleep(lockHold)
	}
}

// TestOpenExclusiveThenShared creates a file, then creates a
// exclusive lock on that file, and verifies this works.  Next
// a sub-process is spawned which tries to open a shared lock
// on this file.  This is expected to fail.
func TestOpenExclusiveThenOpenShared(t *testing.T) {
	testPath, cleanup := createTestPath(t)
	defer cleanup()

	// get shared lock before we start
	f, err := OpenExclusive(testPath, os.O_CREATE|os.O_RDWR, os.FileMode(0600))
	if err != nil {
		t.Fatalf("error getting shared lock to start: %v", err)
	}
	defer f.Close()

	// invoke another process to get an shared lock on this path (expect error)
	cmd := exec.Command(os.Args[0], "-test.run", "TestSubProcessHelper")
	cmd.Env = append(os.Environ(),
		pathEnv+"="+testPath,
		expectEnv+"="+expectError)
	err = cmd.Start()
	if err != nil {
		t.Fatalf("error starting other process: %v", err)
	}

	// now wait for other process to finish
	err = cmd.Wait()
	if err != nil {
		t.Fatalf("other command returned error: %v", err)
	}
}

// TestOpenSharedMultiProcess creates a file, then creates a
// shared lock on that file.  Then a sub-process is started
// which attempts to also create a shared lock on that same
// file, this is expected to succeed.
func TestOpenSharedThenOpenShared(t *testing.T) {
	testPath, cleanup := createTestPath(t)
	defer cleanup()

	// get shared lock before we start
	f, err := OpenShared(testPath, os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("error getting shared lock to start: %v", err)
	}
	err = f.Close()
	if err != nil {
		t.Fatalf("error closing file, releasing lock: %v", err)
	}

	// invoke another process to get an shared lock on this path
	cmd := exec.Command(os.Args[0], "-test.run", "TestSubProcessHelper")
	cmd.Env = append(os.Environ(),
		pathEnv+"="+testPath)
	err = cmd.Start()
	if err != nil {
		t.Fatalf("error starting other process: %v", err)
	}

	// now wait for other process to finish
	err = cmd.Wait()
	if err != nil {
		t.Fatalf("other command returned error: %v", err)
	}
}

// TestOpenExclusiveMultiProcess creates a file, then creates a
// exclusive lock on that file, and verifies this works.  This
// lock is then released.  Next a sub-process is spawned which
// creates an exclusive lock on that file.  While this lock is
// held the original process attempts to create an exclusive lock
// as well, this is expected to fail.  Finally, after the
// sub-process has exited, we confirm that the original process
// can once again create an exclusive lock on the file.
func TestOpenExclusiveThenOpenExclusive(t *testing.T) {
	testPath, cleanup := createTestPath(t)
	defer cleanup()

	// get exclusive lock ourselves first
	f, err := OpenExclusive(testPath, os.O_CREATE|os.O_RDWR, os.FileMode(0600))
	if err != nil {
		t.Fatalf("error getting exclusive lock to start: %v", err)
	}
	err = f.Close()
	if err != nil {
		t.Fatalf("error closing file, releasing lock: %v", err)
	}

	// invoke another process to get an exclusive lock on this path
	cmd := exec.Command(os.Args[0], "-test.run", "TestSubProcessHelper")
	cmd.Env = append(
		os.Environ(),
		pathEnv+"="+testPath,
		modeEnv+"="+modeEnv)
	err = cmd.Start()
	if err != nil {
		t.Fatalf("error starting other process: %v", err)
	}

	// wait for the other command to boot up
	time.Sleep(testBoot)

	// try to get the lock again (should fail)
	_, err = OpenExclusive(testPath, os.O_CREATE|os.O_RDWR, os.FileMode(0600))
	if err == nil {
		t.Fatalf("expected to fail getting lock in the middle, succeeded")
	}

	// now wait for other process to finish
	err = cmd.Wait()
	if err != nil {
		t.Fatalf("other command returned error: %v", err)
	}

	// finally ensure we can get exclusive lock ourselves again
	f3, err := OpenExclusive(testPath, os.O_CREATE|os.O_RDWR, os.FileMode(0600))
	if err != nil {
		t.Fatalf("error getting exclusive lock at end: %v", err)
	}
	err = f3.Close()
	if err != nil {
		t.Fatalf("error closing file, releasing lock: %v", err)
	}
}

func createTestPath(t *testing.T) (path string, cleanup func()) {
	// create a path to test
	tmpDir, err := os.MkdirTemp("", "lock-test")
	if err != nil {
		t.Fatalf("error creating temp dir: %v", err)
	}
	path = filepath.Join(tmpDir, "file")
	err = os.WriteFile(path, []byte("file"), 0600)
	if err != nil {
		t.Fatalf("error creatig temp file '%s': %v", path, err)
	}
	cleanup = func() {
		err = os.RemoveAll(tmpDir)
		if err != nil {
			t.Fatalf("error cleaning up test: %v", err)
		}
	}
	return path, cleanup
}
