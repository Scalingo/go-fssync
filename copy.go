package fssync

import (
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// copyContent is highly inspired from io.Copy, but calls to fadvise have been
// added to prevent caching the whole content of the files during the process,
// impacting the whole OS disk cache
func (s *FsSyncer) copyContent(src, dst *os.File) (written int64, err error) {
	buf := make([]byte, s.bufferSize)
	for {
		nr, er := src.Read(buf)
		if nr > 0 {
			if s.noCache {
				unix.Fadvise(int(src.Fd()), written, written+int64(nr), unix.FADV_DONTNEED)
			}

			nw, ew := dst.Write(buf[0:nr])
			if nw > 0 {
				if s.noCache {
					unix.Fadvise(int(dst.Fd()), written, written+int64(nw), unix.FADV_DONTNEED)
				}
				written += int64(nw)
			}
			if ew != nil {
				err = ew
				break
			}
			if nr != nw {
				err = io.ErrShortWrite
				break
			}
		}
		if er != nil {
			if er != io.EOF {
				err = er
			}
			break
		}
	}
	return written, err
}
