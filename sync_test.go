package fssync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	// Directory where fixtures will be synced during the tests execution
	tmpDir = ".tmp"
	// Root directory for the fixture files
	fixturesRootDir = "test-fixtures"
)

func TestMain(m *testing.M) {
	err := os.MkdirAll(tmpDir, 0700)
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmpDir)
	m.Run()
}

func TestFsSyncer_Sync(t *testing.T) {
	tests := map[string]struct {
		fixtureSrc      string
		fixtureDst      string
		expectedChanges []string
		syncOptions     []func(*FsSyncer)
		additionalSpecs func(t *testing.T, src, dst string)
	}{
		"it should copy a file": {
			fixtureSrc: "src/file",
		},
		"it should copy a directory": {
			fixtureSrc: "src/dir",
		},
		"it should copy a symlink": {
			fixtureSrc: "src/symlink",
		},
		"it should change target of symlink if relative to src dir": {
			fixtureSrc: "src/local-symlink",
		},
		"it should keep the symlink to a relative path": {
			fixtureSrc: "src/relative-symlink",
		},
		"it should not replace a file with the same content but not the same mtime if checksum checks are enabled": {
			fixtureSrc:      "src/file",
			fixtureDst:      "dst/cp-file",
			expectedChanges: []string{},
			syncOptions:     []func(*FsSyncer){WithChecksum},
		},
		"it should replace a file with the same mtime but not the same size": {
			fixtureSrc:      "src/file",
			fixtureDst:      "dst/mtime-file",
			expectedChanges: []string{"a"},
		},
		"it should replace a file by a directory": {
			fixtureSrc:      "src/dir",
			fixtureDst:      "dst/replace-dir",
			expectedChanges: []string{"dir1"},
		},
		"it should replace a directory by a file": {
			fixtureSrc:      "src/file",
			fixtureDst:      "dst/replace-file",
			expectedChanges: []string{"a"},
		},
		"it should delete extraneous files": {
			fixtureSrc:      "src/file",
			fixtureDst:      "dst/extraneous-files",
			expectedChanges: []string{"b", "dir", "dir/c"},
			additionalSpecs: func(t *testing.T, src, dst string) {
				_, err := os.Stat(filepath.Join(dst, "b"))
				assert.True(t, os.IsNotExist(err))
				_, err = os.Stat(filepath.Join(dst, "dir"))
				assert.True(t, os.IsNotExist(err))
				_, err = os.Stat(filepath.Join(dst, "c"))
				assert.True(t, os.IsNotExist(err))
			},
		},
	}

	for msg, test := range tests {
		t.Run(msg, func(t *testing.T) {
			// Given
			if test.syncOptions == nil {
				test.syncOptions = []func(*FsSyncer){}
			}
			syncer := New(test.syncOptions...)

			// Create the directory that will be the destination for the tests on fixture files
			dst, err := os.MkdirTemp("./"+tmpDir, "fssync-test")
			assert.NoError(t, err)
			defer assert.NoError(t, os.RemoveAll(dst))

			if test.fixtureDst != "" {
				fixtureDst := filepath.Join(fixturesRootDir, test.fixtureDst)
				fixtureSyncer := New()
				_, err := fixtureSyncer.Sync(dst, fixtureDst)
				if err != nil {
					assert.NoError(t, err)
				}
			}

			// When
			src := filepath.Join(fixturesRootDir, test.fixtureSrc)
			syncReport, err := syncer.Sync(dst, src)
			assert.NoError(t, err)

			// Then
			if test.expectedChanges != nil {
				// Check that if there is no change expected, there should be no change in report
				if len(test.expectedChanges) == 0 {
					assert.Equal(t, syncReport.ChangeCount(), 0)
				}

				for _, path := range test.expectedChanges {
					t.Run(fmt.Sprintf("file %s has changed", path), func(t *testing.T) {
						assert.True(t, syncReport.HasChanged(filepath.Join(dst, path)))
					})
				}
			}

			err = filepath.Walk(src, func(path string, srcInfo os.FileInfo, err error) error {
				if err != nil {
					return err
				}

				t.Run("with file "+path, func(t *testing.T) {
					dstPath := strings.Replace(path, src, dst, 1)
					dstStat, err := os.Lstat(dstPath)
					assert.NoError(t, err)

					assert.Equal(t, srcInfo.IsDir(), dstStat.IsDir(), "must be a directory")
					assert.Equal(t,
						srcInfo.Mode(), dstStat.Mode(),
						"file mode is different ("+srcInfo.Mode().String()+" != "+dstStat.Mode().String()+")",
					)
					// If source file is a symlink
					if srcInfo.Mode()&os.ModeSymlink == os.ModeSymlink {
						srcLink, err := os.Readlink(path)
						assert.NoError(t, err)
						dstLink, err := os.Readlink(dstPath)
						assert.NoError(t, err)

						// If link is mentioning src path, replace it with dst
						expectedLink := srcLink
						if strings.Contains(expectedLink, src) {
							expectedLink = strings.Replace(dstLink, src, dst, 1)
						}

						assert.Equal(t, expectedLink, dstLink)
					} else {
						assert.Equal(t, srcInfo.Size(), dstStat.Size(), "size is different")
						assert.Equal(t, srcInfo.ModTime(), dstStat.ModTime(), "modification time is different")
					}
				})
				return nil
			})
			assert.NoError(t, err)

			if test.additionalSpecs != nil {
				test.additionalSpecs(t, src, dst)
			}
		})
	}
}
