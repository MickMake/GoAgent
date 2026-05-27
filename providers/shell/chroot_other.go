//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd

package shell

import (
	"fmt"
	"os/exec"
)

func applyChroot(cmd *exec.Cmd, chroot string) error {
	if chroot == "" {
		return nil
	}

	return fmt.Errorf("chroot is not supported on this platform")
}
