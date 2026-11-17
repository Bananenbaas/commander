package action

import (
	"os/exec"
	"syscall"
)

type linuxLauncher struct{}

// NewLauncher returns a launcher that starts detached child processes.
func NewLauncher() Launcher { return linuxLauncher{} }

func (linuxLauncher) Run(program string, args []string) error {
	return start(exec.Command(program, args...))
}

func (linuxLauncher) Open(target string) error {
	return start(exec.Command("xdg-open", target))
}

// start launches the command in its own session and reaps it in the
// background so no zombie is left behind.
func start(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
