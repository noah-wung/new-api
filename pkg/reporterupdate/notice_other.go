//go:build !darwin && !windows

package reporterupdate

import (
	"context"
	"errors"
)

func NotifyUpgrade(context.Context, string, string) error {
	return errors.New("desktop_notification_unavailable")
}
