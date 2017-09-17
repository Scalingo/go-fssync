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

type statTimes struct {
	atime time.Time
	mtime time.Time
}

func (s *FsSyncer) Sync(src, dst string) error {
	timesMap := map[string]statTimes{}
	inoMap := map[uint64]string{}

	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		dstPath := strings.Replace(path, src, dst, 1)

		srcStat, ok := info.Sys().(*syscall.Stat_t)
		if existingLink, ok := inoMap[srcStat.Ino]; ok {
			err = os.Link(existingLink, dstPath)
			if err != nil {
				return errors.Wrapf(err, "fail to create link from %v to %v", existingLink, dstPath)
			}
			return nil
		} else {
			inoMap[srcStat.Ino] = dstPath
		}

		atime := time.Unix(int64(srcStat.Atim.Sec), int64(srcStat.Atim.Nsec))
		mtime := time.Unix(int64(srcStat.Mtim.Sec), int64(srcStat.Mtim.Nsec))
		if !ok {
			return errors.Wrapf(err, "fail to get detaied stat info for %s", src)
		}

		_, err = os.Lstat(dstPath)
		if err != nil && !os.IsNotExist(err) {
			return errors.Wrapf(err, "fail to stat %v", dstPath)
		} else if os.IsNotExist(err) {
			if info.IsDir() {
				err := os.MkdirAll(dstPath, info.Mode())
				if err != nil {
					return errors.Wrapf(err, "fail to create dst directory %v", dstPath)
				}
				timesMap[dstPath] = statTimes{atime: atime, mtime: mtime}
			} else if info.Mode()&os.ModeSymlink == os.ModeSymlink {
				linkDst, err := os.Readlink(path)
				if err != nil {
					return errors.Wrapf(err, "fail to get link destination of src %v", src)
				}
				err = os.Symlink(linkDst, dstPath)
				if err != nil {
					return errors.Wrapf(err, "fail to create symlink %v (%v)", dstPath, linkDst)
				}

			} else {
				_, err := s.copyFileContent(path, dstPath, info)
				if err != nil {
					return errors.Wrapf(err, "fail to copy content from %v to %v", path, dstPath)
				}
				timesMap[dstPath] = statTimes{atime: atime, mtime: mtime}
			}
		}

		return nil
	})

	if err != nil {
		return errors.Wrapf(err, "fail to walk %v", src)
	}

	for file, times := range timesMap {
		err = os.Chtimes(file, times.atime, times.mtime)
		if err != nil {
			return errors.Wrapf(err, "fail to set atime and mtime of %v", file)
		}
	}

	return nil
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
