package fssync

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/pkg/errors"
)

type SyncReport interface {
	HasChanged(file string) bool
	ChangeCount() int
}

type Syncer interface {
	Sync(src, dst string) (SyncReport, error)
}

type FsSyncer struct {
	checkChecksum     bool
	preserveOwnership bool
	ignoreNotFound    bool
	noCache           bool
	bufferSize        int64
}

type fsSyncReport struct {
	fileChanges map[string]bool
}

func (r fsSyncReport) HasChanged(file string) bool {
	return r.fileChanges[file]
}

func (r fsSyncReport) ChangeCount() int {
	return len(r.fileChanges)
}

func New(opts ...func(*FsSyncer)) *FsSyncer {
	s := &FsSyncer{
		bufferSize: 512 * 1024,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// WithChecksum option: Check SHA1 checksum instead of modtime + size
func WithChecksum(s *FsSyncer) {
	s.checkChecksum = true
}

// PreserveOwnership option: chown files from source owner instead of copying
// with current owner root required to change the user ownership in most cases
func PreserveOwnership(s *FsSyncer) {
	s.preserveOwnership = true
}

// IgnoreNotFound option: if the synced directory is heavily used during the
// sync there might be a file which is walked in but which does not exist
// anymore when Lstat is used
func IgnoreNotFound(s *FsSyncer) {
	s.ignoreNotFound = true
}

// NoCache option: Use the system call fadvise to discard kernel cache after
// reading/writing Inspired from
// https://github.com/coreutils/coreutils/blob/master/src/dd.c
func NoCache(s *FsSyncer) {
	s.noCache = true
}

// WithBufferSize option: lets you configure the size of the memory buffer used
// to perform the copy from one file to another
// Default is 512kB
func WithBufferSize(n int64) func(*FsSyncer) {
	return func(s *FsSyncer) {
		s.bufferSize = n
	}
}

type syncInfo struct {
	base     string
	path     string
	fileInfo os.FileInfo
	stat     *syscall.Stat_t
	times    statTimes
}

func (s syncInfo) SHA1() ([]byte, error) {
	hash := sha1.New()
	fd, err := os.Open(s.path)
	if err != nil {
		return nil, errors.Wrapf(err, "fail to open file")
	}
	defer fd.Close()
	_, err = io.Copy(hash, fd)
	if err != nil {
		return nil, errors.Wrapf(err, "fail to read file content")
	}
	return hash.Sum(nil), nil
}

type syncState struct {
	timesMap map[string]statTimes
	inoMap   map[uint64]string
}

type statTimes struct {
	atime time.Time
	mtime time.Time
}

type existingFileRes struct {
	shouldUpdateTimes bool
	hasContentChanged bool
}

type unexistingFileRes struct {
	shouldUpdateTimes bool
}

func (s *FsSyncer) Sync(src, dst string) (SyncReport, error) {
	state := syncState{
		timesMap: map[string]statTimes{},
		inoMap:   map[uint64]string{},
	}
	report := fsSyncReport{fileChanges: map[string]bool{}}

	src = filepath.Clean(src)
	dst = filepath.Clean(dst)

	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) && s.ignoreNotFound {
				return nil
			}
			return err
		}
		dstPath := strings.Replace(path, src, dst, 1)

		srcSysStat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return errors.Wrapf(err, "fail to get detailed stat info for %s", path)
		}
		atime := time.Unix(int64(srcSysStat.Atim.Sec), int64(srcSysStat.Atim.Nsec))
		mtime := time.Unix(int64(srcSysStat.Mtim.Sec), int64(srcSysStat.Mtim.Nsec))

		dstStat, err := os.Lstat(dstPath)
		if os.IsNotExist(err) {
			report.fileChanges[dstPath] = true
			res, err := s.syncUnexistingFile(syncInfo{
				base:     src,
				path:     path,
				fileInfo: info,
				stat:     srcSysStat,
			}, syncInfo{
				base: dst,
				path: dstPath,
			}, state)
			if err != nil {
				return errors.Wrapf(err, "fail to handle unexisting file %v", path)
			}
			if res.shouldUpdateTimes {
				state.timesMap[dstPath] = statTimes{atime: atime, mtime: mtime}
			}
			if s.preserveOwnership {
				err = os.Chown(dstPath, int(srcSysStat.Uid), int(srcSysStat.Gid))
				if err != nil {
					return errors.Wrapf(err, "fail to chown %v", dstPath)
				}
			}
			return nil
		} else if err != nil {
			return errors.Wrapf(err, "fail to stat %v", dstPath)
		}

		dstSysStat, ok := dstStat.Sys().(*syscall.Stat_t)
		if !ok {
			return errors.Wrapf(err, "fail to get detailed stat info for %s", dstPath)
		}
		dstatime := time.Unix(int64(dstSysStat.Atim.Sec), int64(dstSysStat.Atim.Nsec))
		dstmtime := time.Unix(int64(dstSysStat.Mtim.Sec), int64(dstSysStat.Mtim.Nsec))

		res, err := s.syncExistingFile(syncInfo{
			base:     src,
			path:     path,
			fileInfo: info,
			stat:     srcSysStat,
			times:    statTimes{atime: atime, mtime: mtime},
		}, syncInfo{
			base:     dst,
			path:     dstPath,
			fileInfo: dstStat,
			stat:     dstSysStat,
			times:    statTimes{atime: dstatime, mtime: dstmtime},
		}, state)
		if err != nil {
			return errors.Wrapf(err, "fail to sync existing file %v", path)
		}
		if res.shouldUpdateTimes {
			state.timesMap[dstPath] = statTimes{atime: atime, mtime: mtime}
		}
		if res.hasContentChanged {
			report.fileChanges[dstPath] = true
		}
		if s.preserveOwnership {
			err = os.Chown(dstPath, int(srcSysStat.Uid), int(srcSysStat.Gid))
			if err != nil {
				return errors.Wrapf(err, "fail to chown %v", dstPath)
			}
		}
		return nil
	})

	if err != nil {
		return report, errors.Wrapf(err, "fail to walk %v", src)
	}

	dirsToRemove := []string{}
	err = filepath.Walk(dst, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		srcPath := strings.Replace(path, dst, src, 1)
		_, err = os.Lstat(srcPath)
		if os.IsNotExist(err) {
			report.fileChanges[path] = true
			if info.IsDir() {
				// Do not delete directory straight we want to tag all files
				// recursively before deleting empty dirs
				dirsToRemove = append(dirsToRemove, path)
			} else {
				err := os.Remove(path)
				if err != nil {
					return errors.Wrapf(err, "fail to delete %v", path)
				}
			}
		}
		return nil
	})
	if err != nil {
		return report, errors.Wrapf(err, "fail to walk %v", dst)
	}

	for i := len(dirsToRemove) - 1; i >= 0; i-- {
		dir := dirsToRemove[i]
		err := os.Remove(dir)
		if err != nil {
			return report, errors.Wrapf(err, "fail to delete %v", dir)
		}
	}

	// Change times after removing entries as removing a file
	// changes the mtime at the os level
	for file, times := range state.timesMap {
		err = os.Chtimes(file, times.atime, times.mtime)
		if err != nil && !(os.IsNotExist(err) && s.ignoreNotFound) {
			return report, errors.Wrapf(err, "fail to set atime and mtime of %v", file)
		}
	}

	return report, nil
}

func (s *FsSyncer) syncExistingFile(src, dst syncInfo, state syncState) (existingFileRes, error) {
	res := existingFileRes{}
	if src.fileInfo.IsDir() && dst.fileInfo.IsDir() {
		res.shouldUpdateTimes = true
		return res, nil
	} else if src.fileInfo.IsDir() && !dst.fileInfo.IsDir() ||
		!src.fileInfo.IsDir() && dst.fileInfo.IsDir() {
		err := os.RemoveAll(dst.path)
		if err != nil {
			return res, errors.Wrapf(err, "fail to remove destination invalid file %v", dst.path)
		}
	}

	if s.checkChecksum {
		srcSHA1, err := src.SHA1()
		if err != nil {
			return res, errors.Wrapf(err, "fail to compute SHA1 of %v", src.path)
		}
		dstSHA1, err := dst.SHA1()
		if err != nil {
			return res, errors.Wrapf(err, "fail to compute SHA1 of %v", dst.path)
		}
		if bytes.Equal(srcSHA1, dstSHA1) {
			res.shouldUpdateTimes = true
			return res, nil
		}
	} else {
		if src.fileInfo.Size() == dst.fileInfo.Size() && src.fileInfo.ModTime() == dst.fileInfo.ModTime() {
			return res, nil
		}
	}

	res.hasContentChanged = true
	dir := filepath.Dir(dst.path)
	base := filepath.Base(dst.path)
	tmpDst := tmpFileName(dir, base)
	newFileRes, err := s.syncUnexistingFile(src, syncInfo{base: dst.base, path: tmpDst}, state)
	if err != nil {
		return res, errors.Wrapf(err, "fail to sync src to temp file %v -> %v", src.path, tmpDst)
	}
	res.shouldUpdateTimes = newFileRes.shouldUpdateTimes

	// Once the new file is ready, replace the old one
	err = os.Rename(tmpDst, dst.path)
	if err != nil {
		return res, errors.Wrapf(err, "fail to mv tmp file on original file %v -> %v", tmpDst, dst.path)
	}
	// temp file name has been set to state, restore it to real name
	state.inoMap[src.stat.Ino] = dst.path

	return res, nil
}

func (s *FsSyncer) syncUnexistingFile(src, dst syncInfo, state syncState) (unexistingFileRes, error) {
	res := unexistingFileRes{}

	if existingLink, ok := state.inoMap[src.stat.Ino]; ok {
		err := os.Link(existingLink, dst.path)
		if err != nil {
			return res, errors.Wrapf(err, "fail to create link from %v to %v", existingLink, dst.path)
		}
		return res, nil
	}

	state.inoMap[src.stat.Ino] = dst.path

	if src.fileInfo.IsDir() {
		err := os.MkdirAll(dst.path, src.fileInfo.Mode())
		if err != nil {
			return res, errors.Wrapf(err, "fail to create dst directory %v", dst.path)
		}
		return unexistingFileRes{shouldUpdateTimes: true}, nil
	}

	if src.fileInfo.Mode()&os.ModeSymlink == os.ModeSymlink {
		linkDst, err := os.Readlink(src.path)
		if err != nil {
			return res, errors.Wrapf(err, "fail to get link destination of src %v", src.path)
		}
		if strings.Contains(linkDst, src.base) {
			linkDst = strings.Replace(linkDst, src.base, dst.base, 1)
		}
		err = os.Symlink(linkDst, dst.path)
		if err != nil {
			return res, errors.Wrapf(err, "fail to create symlink %v (%v)", dst.path, linkDst)
		}
		return res, nil
	}

	_, err := s.copyFileContent(src.path, dst.path, src.fileInfo)
	if err != nil {
		return res, errors.Wrapf(err, "fail to copy content from %v to %v", src.path, dst.path)
	}

	return unexistingFileRes{shouldUpdateTimes: true}, nil
}

func (s *FsSyncer) copyFileContent(src, dst string, info os.FileInfo) (int64, error) {
	sfd, err := os.Open(src)
	if err != nil {
		return -1, errors.Wrapf(err, "fail to open src %v", src)
	}
	defer sfd.Close()
	fd, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY, info.Mode())
	if err != nil {
		return -1, errors.Wrapf(err, "fail to open dest %v", dst)
	}
	defer fd.Close()
	n, err := s.copyContent(sfd, fd)
	if err != nil {
		return -1, errors.Wrapf(err, "fail to copy data")
	}
	return n, nil
}

func tmpFileName(dir, base string) string {
	// From io/ioutil.TempFile
	r := uint32(time.Now().UnixNano() + int64(os.Getpid()))
	r = r*1664525 + 1013904223 // constants from Numerical Recipes
	return filepath.Join(dir, fmt.Sprintf(".%s-%s", base, strconv.Itoa(int(1e9 + r%1e9))[1:]))
}
