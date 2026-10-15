//go:build !darwin && !windows

package reporterupdate

import (
	"context"
	"errors"
	"os/exec"
)

func newPlatform(string) (taskPlatform, error) {
	return nil, errors.New("automatic updates require macOS or Windows")
}
func hideProcess(*exec.Cmd)                        {}
func stopLegacyTask(context.Context, string) error { return errors.New("unsupported_platform") }
