FS Sync
=======

Tool which aims at syncing copying one file tree to another in a clever way. A bit like

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
// Check with checksum instead of size/mtime
fssync.WithChecksum

// Preserve Owner/Group (require root), will return an error otherwise
fssync.PreserveOwnership

// Ignore if files are being deleted during the sync (if a process is
// creating/destroyging quickly files in the source, it might happen)
fssync.IgnoreNotFound
```

By default the copy is based on the size + modification date
