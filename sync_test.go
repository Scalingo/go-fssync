package fssync

import (
	"fmt"
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
		ExpectedChanges []string
		SyncOptions     []func(*FsSyncer)
		AdditionalSpecs func(t *testing.T, src, dst string)
	}{
		{
			Name:       "it should copy a file",
			FixtureSrc: "src/file",
		}, {
			Name:       "it should copy a directory",
			FixtureSrc: "src/dir",
		}, {
			Name:       "it should copy a hardlink",
			FixtureSrc: "src/hardlink",
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
			Name:       "it should copy a symlink",
			FixtureSrc: "src/symlink",
		}, {
			Name:       "it should change target of symlink if relative to src dir",
			FixtureSrc: "src/local-symlink",
		}, {
			Name:       "it should keep the symlink to a relative path",
			FixtureSrc: "src/relative-symlink",
		}, {
			Name:            "it should replace a file with the same content but not the same mtime",
			FixtureSrc:      "src/file",
			FixtureDst:      "dst/cp-file",
			ExpectedChanges: []string{"a"},
		}, {
			Name:            "it should replace a file with the same size but not the same mtime",
			FixtureSrc:      "src/file",
			FixtureDst:      "dst/same-size",
			ExpectedChanges: []string{"a"},
		}, {
			Name:            "it should not replace a file with the same content but not the same mtime if checksum checks are enabled",
			FixtureSrc:      "src/file",
			FixtureDst:      "dst/cp-file",
			ExpectedChanges: []string{},
			SyncOptions:     []func(*FsSyncer){WithChecksum},
		}, {
			Name:            "it should not replace a file with the same size and mtime",
			FixtureSrc:      "src/file",
			FixtureDst:      "dst/rsync-file",
			ExpectedChanges: []string{},
		}, {
			Name:            "it should replace a file with the same mtime but not the same size",
			FixtureSrc:      "src/file",
			FixtureDst:      "dst/mtime-file",
			ExpectedChanges: []string{"a"},
		}, {
			Name:            "it should replace a file by a directory",
			FixtureSrc:      "src/dir",
			FixtureDst:      "dst/replace-dir",
			ExpectedChanges: []string{"dir1"},
		}, {
			Name:            "it should replace a directory by a file",
			FixtureSrc:      "src/file",
			FixtureDst:      "dst/replace-file",
			ExpectedChanges: []string{"a"},
		}, {
			Name:            "it should delete extraneous files",
			FixtureSrc:      "src/file",
			FixtureDst:      "dst/extraneous-files",
			ExpectedChanges: []string{"b", "dir", "dir/c"},
			AdditionalSpecs: func(t *testing.T, src, dst string) {
				_, err := os.Stat(filepath.Join(dst, "b"))
				assert.True(t, os.IsNotExist(err))
				_, err = os.Stat(filepath.Join(dst, "dir"))
				assert.True(t, os.IsNotExist(err))
				_, err = os.Stat(filepath.Join(dst, "c"))
				assert.True(t, os.IsNotExist(err))
			},
		},
	}

	for _, example := range examples {
		t.Run(example.Name, func(t *testing.T) {
			if example.SyncOptions == nil {
				example.SyncOptions = []func(*FsSyncer){}
			}
			syncer := New(example.SyncOptions...)

			dst, err := ioutil.TempDir("./.tmp", "fssync-test")
			assert.NoError(t, err)
			defer assert.NoError(t, os.RemoveAll(dst))
			if example.FixtureDst != "" {
				fixtureDst := filepath.Join("test-fixtures", example.FixtureDst)
				fixtureSyncer := New()
				_, err := fixtureSyncer.Sync(fixtureDst, dst)
				if err != nil {
					assert.NoError(t, err)
				}
			}

			src := filepath.Join("test-fixtures", example.FixtureSrc)
			report, err := syncer.Sync(src, dst)
			assert.NoError(t, err)

			if example.ExpectedChanges != nil {
				// Check that if there is no change expected, there should be no change in report
				if len(example.ExpectedChanges) == 0 {
					assert.Equal(t, report.ChangeCount(), 0)
				}

				for _, path := range example.ExpectedChanges {
					t.Run(fmt.Sprintf("file %s has changed", path), func(t *testing.T) {
						assert.True(t, report.HasChanged(filepath.Join(dst, path)))
					})
				}
			}

			err = filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}

				t.Run("with file "+path, func(t *testing.T) {
					dstPath := strings.Replace(path, src, dst, 1)
					dstStat, err := os.Lstat(dstPath)
					assert.NoError(t, err)
					assert.Equal(t, info.IsDir(), dstStat.IsDir())
					assert.Equal(t, info.Mode(), dstStat.Mode())
					if info.Mode()&os.ModeSymlink == os.ModeSymlink {
						srcLink, err := os.Readlink(path)
						assert.NoError(t, err)
						dstLink, err := os.Readlink(dstPath)
						assert.NoError(t, err)

						// If link is mentionning src path, replace it with dst
						expectedLink := srcLink
						if strings.Contains(expectedLink, src) {
							expectedLink = strings.Replace(dstLink, src, dst, 1)
						}

						assert.Equal(t, expectedLink, dstLink)
					} else {
						assert.Equal(t, info.Size(), dstStat.Size())
						assert.Equal(t, info.ModTime(), dstStat.ModTime())
					}
				})
				return nil
			})
			assert.NoError(t, err)

			if example.AdditionalSpecs != nil {
				example.AdditionalSpecs(t, src, dst)
			}
		})
	}
}
