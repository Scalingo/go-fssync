package fssync

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/pkg/errors"
)

type Syncer interface {
	Sync(src, dst string) error
}

type FsSyncer struct {
	CheckChecksum     bool
	PreserveOwnership bool
}

func New(opts ...func(*FsSyncer)) *FsSyncer {
	s := &FsSyncer{}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func WithChecksum(s *FsSyncer) {
	s.CheckChecksum = true
}

func PreserveOwnership(s *FsSyncer) {
	s.PreserveOwnership = true
}

type syncInfo struct {
	path     string
	fileInfo os.FileInfo
	stat     *syscall.Stat_t
}

type syncState struct {
	timesMap map[string]statTimes
	inoMap   map[uint64]string
}

type statTimes struct {
	atime time.Time
	mtime time.Time
}

type unexistingFileRes struct {
	shouldUpdateTimes bool
}

func (s *FsSyncer) Sync(src, dst string) error {
	state := syncState{
		timesMap: map[string]statTimes{},
		inoMap:   map[uint64]string{},
	}

	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		dstPath := strings.Replace(path, src, dst, 1)

		srcStat, ok := info.Sys().(*syscall.Stat_t)
		atime := time.Unix(int64(srcStat.Atim.Sec), int64(srcStat.Atim.Nsec))
		mtime := time.Unix(int64(srcStat.Mtim.Sec), int64(srcStat.Mtim.Nsec))
		if !ok {
			return errors.Wrapf(err, "fail to get detaied stat info for %s", src)
		}

		_, err = os.Lstat(dstPath)
		if os.IsNotExist(err) {
			res, err := s.syncUnexistingFile(syncInfo{
				path:     path,
				fileInfo: info,
				stat:     srcStat,
			}, syncInfo{
				path: dstPath,
			}, state)
			if err != nil {
				return errors.Wrapf(err, "fail to handle unexisting file %v", path)
			}
			if res.shouldUpdateTimes {
				state.timesMap[dstPath] = statTimes{atime: atime, mtime: mtime}
			}
		} else if err != nil {
			return errors.Wrapf(err, "fail to stat %v", dstPath)
		}
		return nil
	})

	if err != nil {
		return errors.Wrapf(err, "fail to walk %v", src)
	}

	for file, times := range state.timesMap {
		err = os.Chtimes(file, times.atime, times.mtime)
		if err != nil {
			return errors.Wrapf(err, "fail to set atime and mtime of %v", file)
		}
	}

	return nil
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
	n, err := io.Copy(fd, sfd)
	if err != nil {
		return -1, errors.Wrapf(err, "fail to copy data")
	}
	return n, nil
}
