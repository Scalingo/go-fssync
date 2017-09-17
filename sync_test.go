package fssync

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMain(m *testing.M) {
	err := os.MkdirAll(".tmp", 0700)
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(".tmp")
	m.Run()
}

func TestFsSyncer_Sync(t *testing.T) {
	examples := []struct {
		Name            string
		FixtureSrc      string
		FixtureDst      string
		ExpectedDst     string
		AdditionalSpecs func(t *testing.T, src, dst string)
	}{
		{
			Name:       "it should copy a file",
			FixtureSrc: "file",
		}, {
			Name:       "it should copy a directory",
			FixtureSrc: "dir",
		}, {
			Name:       "it should copy a hardlink",
			FixtureSrc: "hardlink",
			AdditionalSpecs: func(t *testing.T, src, dst string) {
				apath := filepath.Join(dst, "a")
				bpath := filepath.Join(dst, "b")
				astat, err := os.Lstat(apath)
				assert.NoError(t, err)
				bstat, err := os.Lstat(bpath)
				assert.NoError(t, err)
				asysstat := astat.Sys().(*syscall.Stat_t)
				bsysstat := bstat.Sys().(*syscall.Stat_t)
				assert.Equal(t, asysstat.Ino, bsysstat.Ino)
			},
		}, {
			Name:       "it should copy a softlink",
			FixtureSrc: "softlink",
		},
	}

	for _, example := range examples {
		t.Run(example.Name, func(t *testing.T) {
			syncer := New()

			if example.ExpectedDst == "" {
				dir, err := ioutil.TempDir("./.tmp", "fssync-test")
				assert.NoError(t, err)
				defer assert.NoError(t, os.RemoveAll(dir))
				example.ExpectedDst = dir
			}

			src := filepath.Join("test-fixtures", example.FixtureSrc)
			err := syncer.Sync(src, example.ExpectedDst)
			assert.NoError(t, err)

			err = filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				dstPath := strings.Replace(path, src, example.ExpectedDst, 1)
				dstStat, err := os.Lstat(dstPath)
				assert.NoError(t, err)
				assert.Equal(t, info.IsDir(), dstStat.IsDir())
				assert.Equal(t, info.Mode(), dstStat.Mode())
				assert.Equal(t, info.Size(), dstStat.Size())
				if info.Mode()&os.ModeSymlink != os.ModeSymlink {
					assert.Equal(t, info.ModTime(), dstStat.ModTime())
				}
				return nil
			})
			assert.NoError(t, err)

			if example.AdditionalSpecs != nil {
				example.AdditionalSpecs(t, src, example.ExpectedDst)
			}
		})
	}
}
