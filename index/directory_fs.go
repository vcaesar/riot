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

package index

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/blevesearch/mmap-go"
	segment "github.com/vcaesar/bluge_segment_api"
	"github.com/vcaesar/riot/index/lock"
)

const pidFilename = "riot.pid"

type LoadMMapFunc func(f lock.LockedFile) (*segment.Data, io.Closer, error)

type FileSystemDirectory struct {
	path string
	pid  lock.LockedFile

	newDirPerm  os.FileMode
	newFilePerm os.FileMode

	openExclusive func(path string, flag int, perm os.FileMode) (lock.LockedFile, error)
	openShared    func(path string, flag int, perm os.FileMode) (lock.LockedFile, error)

	loadMMapFunc LoadMMapFunc
}

func NewFileSystemDirectory(path string) *FileSystemDirectory {
	return &FileSystemDirectory{
		path:          path,
		openExclusive: lock.OpenExclusive,
		openShared:    lock.OpenShared,
		newDirPerm:    0700,
		newFilePerm:   0600,
		loadMMapFunc:  LoadMMapAlways,
	}
}

func (d *FileSystemDirectory) exists() (bool, error) {
	_, err := os.Stat(d.path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return true, err
}

func (d *FileSystemDirectory) Setup(readOnly bool) error {
	dirExists, err := d.exists()
	if err != nil {
		return fmt.Errorf("error checking if directory exists '%s': %w", d.path, err)
	}
	if !dirExists {
		if readOnly {
			return fmt.Errorf("readOnly, directory does not exist")
		}
		err = os.MkdirAll(d.path, d.newDirPerm)
		if err != nil {
			return fmt.Errorf("error creating directory '%s': %w", d.path, err)
		}
	}
	return nil
}

func (d *FileSystemDirectory) List(kind string) ([]uint64, error) {
	dirEntries, err := os.ReadDir(d.path)
	if err != nil {
		return nil, err
	}

	var rv uint64Slice
	for _, dirEntry := range dirEntries {
		if _, err := dirEntry.Info(); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if filepath.Ext(dirEntry.Name()) != kind {
			continue
		}
		base := dirEntry.Name()
		base = base[:len(base)-len(kind)]
		var epoch uint64
		epoch, err = strconv.ParseUint(base, 16, 64)
		if err != nil {
			return nil, fmt.Errorf("error parsing identifier '%s': %w", base, err)
		}
		rv = append(rv, epoch)
	}

	sort.Sort(sort.Reverse(rv))

	return rv, nil
}

func (d *FileSystemDirectory) Persist(kind string, id uint64, w WriterTo, closeCh chan struct{}) error {
	path := filepath.Join(d.path, d.fileName(kind, id))
	f, err := d.openExclusive(path, os.O_CREATE|os.O_RDWR, d.newFilePerm)
	if err != nil {
		return err
	}

	cleanup := func() {
		_ = f.Close()
		_ = os.Remove(path)
	}

	_, err = w.WriteTo(f.File(), closeCh)
	if err != nil {
		cleanup()
		return err
	}

	err = f.File().Sync()
	if err != nil {
		cleanup()
		return err
	}

	err = f.Close()
	if err != nil {
		cleanup()
		return err
	}

	return nil
}

func LoadMMapAlways(f lock.LockedFile) (*segment.Data, io.Closer, error) {
	mm, err := mmap.Map(f.File(), mmap.RDONLY, 0)
	if err != nil {
		// mmap failed, try to close the file
		_ = f.Close()
		return nil, nil, err
	}

	closeFunc := func() error {
		err := mm.Unmap()
		// try to close file even if unmap failed
		err2 := f.Close()
		if err == nil {
			// try to return first error
			err = err2
		}
		return err
	}

	return segment.NewDataBytes(mm), closerFunc(closeFunc), nil
}

func LoadMMapNever(f lock.LockedFile) (*segment.Data, io.Closer, error) {
	data, err := segment.NewDataFile(f.File())
	if err != nil {
		return nil, nil, fmt.Errorf("error creating data from file: %w", err)
	}
	return data, closerFunc(f.Close), nil
}

func (d *FileSystemDirectory) SetLoadMMapFunc(f LoadMMapFunc) {
	d.loadMMapFunc = f
}

func (d *FileSystemDirectory) Load(kind string, id uint64) (*segment.Data, io.Closer, error) {
	path := filepath.Join(d.path, d.fileName(kind, id))
	f, err := d.openShared(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, nil, err
	}
	return d.loadMMapFunc(f)
}

func (d *FileSystemDirectory) Remove(kind string, id uint64) error {
	return d.remove(kind, id)
}

func (d *FileSystemDirectory) Lock() error {
	pidPath := filepath.Join(d.path, pidFilename)
	var err error
	d.pid, err = d.openExclusive(pidPath, os.O_CREATE|os.O_RDWR, d.newFilePerm)
	if err != nil {
		return fmt.Errorf("unable to obtain exclusive access: %w", err)
	}
	err = d.pid.File().Truncate(0)
	if err != nil {
		return fmt.Errorf("error truncating pid file: %w", err)
	}
	_, err = fmt.Fprintf(d.pid.File(), "%d\n", os.Getpid())
	if err != nil {
		return fmt.Errorf("error writing pid: %w", err)
	}
	err = d.pid.File().Sync()
	if err != nil {
		return fmt.Errorf("error syncing pid file: %w", err)
	}
	return err
}

func (d *FileSystemDirectory) Unlock() error {
	pidPath := filepath.Join(d.path, pidFilename)
	var err error
	err = d.pid.Close()
	if err != nil {
		return fmt.Errorf("error closing pid file: %w", err)
	}
	err = os.RemoveAll(pidPath)
	if err != nil {
		return fmt.Errorf("error removing pid file: %w", err)
	}
	return err
}

func (d *FileSystemDirectory) Stats() (numFilesOnDisk, numBytesUsedDisk uint64) {
	dirEntries, err := os.ReadDir(d.path)
	if err != nil {
		return 0, 0
	}
	for _, dirEntry := range dirEntries {
		fileInfo, err := dirEntry.Info()
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return 0, 0
		}
		if !fileInfo.IsDir() {
			numFilesOnDisk++
			if size := fileInfo.Size(); size > 0 {
				numBytesUsedDisk += uint64(size)
			}
		}
	}
	return numFilesOnDisk, numBytesUsedDisk
}

func (d *FileSystemDirectory) Sync() error {
	return d.syncDir()
}

func (d *FileSystemDirectory) fileName(kind string, id uint64) string {
	return fmt.Sprintf("%012x", id) + kind
}

type uint64Slice []uint64

func (e uint64Slice) Len() int           { return len(e) }
func (e uint64Slice) Swap(i, j int)      { e[i], e[j] = e[j], e[i] }
func (e uint64Slice) Less(i, j int) bool { return e[i] < e[j] }

type closerFunc func() error

func (c closerFunc) Close() error {
	return c()
}
