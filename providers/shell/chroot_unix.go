//go:build linux || darwin || freebsd || openbsd || netbsd

package shell

import (
	"os/exec"
	"syscall"
)

func applyChroot(cmd *exec.Cmd, chroot string) error {
	if chroot == "" {
		return nil
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Chroot: chroot,
	}

	return nil
}
