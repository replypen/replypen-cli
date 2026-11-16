//go:build !windows

package cli

import "syscall"

// openNoFollow makes OpenFile fail on a symlink as the last path component.
const openNoFollow = syscall.O_NOFOLLOW
