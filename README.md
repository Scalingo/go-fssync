# FS Sync Library v1.0.3

`go-fssync` is a Go library which aims at syncing / copying one file tree to another in a clever way. A bit like the `rsync` tool:

```sh
rsync --archive --update --delete --numeric-ids --hard-links ./source ./destination
```

* Copy all files which are different to the destination dir
* Skip files which are identical on the destination
* Delete files on the destination which are not present in the source
* Hardlinks should be preserved (if cross device, file copied otherwise just another link)
* Symlinks should be preserved
* Permissions should be preserved

```go
// Implemented interface
type interface FsSyncer {
	Sync(string, string) error
}

// Constructor
fssync.New(opts... func(*FsSyncer))

// Options
// WithChecksum option: Check SHA1 checksum instead of modtime + size
fssync.WithChecksum

// PreserveOwnership option: chown files from source owner instead of copying
// with current owner root required to change the user ownership in most cases
fssync.PreserveOwnership

// IgnoreNotFound option: if the synced directory is heavily used during the
// sync there might be a file which is walked in but which does not exist
// anymore when Lstat is used
fssync.IgnoreNotFound

// NoCache option: Use the system call fadvise to discard kernel cache after
// reading/writing Inspired from
// https://github.com/coreutils/coreutils/blob/master/src/dd.c
fssync.NoCache

// WithBufferSize option: lets you configure the size of the memory buffer used
// to perform the copy from one file to another
// Default is 512kB
WithBufferSize(n int64)
```

By default the copy is based on the size and modification date.

## Command Line Tool

You can try out the synchronization mechanisms with the command line tool provided with the library:

```sh
go run cmd/fssync/main.go [-no-cache=false] [-buffer-size=0] [-preserve-ownership=false] [-checksum=false] ./src ./dst
```

## Release a New Version

Bump new version number in:

- `CHANGELOG.md`
- `README.md`

Commit, tag and create a new release:

```sh
version="1.0.3"

git switch --create release/${version}
git add CHANGELOG.md README.md
git commit --message="Bump v${version}"
git push --set-upstream origin release/${version}
gh pr create --reviewer=EtienneM --title "$(git log -1 --pretty=%B)"
```

Once the pull request merged, you can tag the new release.

```sh
git tag v${version}
git push origin master v${version}
gh release create v${version}
```

The title of the release should be the version number and the text of the
release is the same as the changelog.
