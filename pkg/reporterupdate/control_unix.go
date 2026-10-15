//go:build !windows

package reporterupdate

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
)

func controlAddress(config string) (string, error) {
	dir := filepath.Join("/tmp", "new-api-reporter-"+strconv.Itoa(os.Getuid()))
	if err := SecureDir(dir); err != nil {
		return "", err
	}
	abs, err := filepath.Abs(config)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, Digest([]byte(abs))[:32]+".sock"), nil
}
func listenControl(config string) (net.Listener, error) {
	address, err := controlAddress(config)
	if err != nil {
		return nil, err
	}
	conn, e := net.DialTimeout("unix", address, controlTimeout)
	if e == nil {
		conn.Close()
		return nil, os.ErrExist
	}
	if err = os.Remove(address); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return net.Listen("unix", address)
}
func dialControl(ctx context.Context, config string) (net.Conn, error) {
	address, err := controlAddress(config)
	if err != nil {
		return nil, err
	}
	d := net.Dialer{}
	return d.DialContext(ctx, "unix", address)
}
