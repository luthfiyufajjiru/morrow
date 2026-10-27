//go:build !windows

package inbox

import (
	"fmt"
	"os"
	"os/user"
	"strconv"
	"syscall"
)

// fileOwner returns who created the file. Only root can change a file's
// owner, so a deployer cannot make its submission look like someone else's.
// Hard-linked files are rejected: a link to another user's file would carry
// that user's ownership.
func fileOwner(_ string, fi os.FileInfo) (submitter, error) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return submitter{}, fmt.Errorf("cannot read file owner")
	}
	if st.Nlink != 1 {
		return submitter{}, fmt.Errorf("file has %d hard links; only plain files are accepted", st.Nlink)
	}
	if st.Uid == 0 {
		return submitter{name: "root", privileged: true}, nil
	}
	u, err := user.LookupId(strconv.FormatUint(uint64(st.Uid), 10))
	if err != nil {
		return submitter{name: "uid:" + strconv.FormatUint(uint64(st.Uid), 10)}, nil
	}
	return submitter{name: u.Username}, nil
}

// openNoFollow opens path for reading without following a symlink swapped in
// after the Lstat check.
func openNoFollow(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}
