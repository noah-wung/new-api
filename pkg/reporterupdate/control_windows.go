package reporterupdate

import (
	"context"
	"net"
	"path/filepath"
	"strings"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

func pipeIdentity(config string) (string, string, error) {
	t, e := windows.OpenCurrentProcessToken()
	if e != nil {
		return "", "", e
	}
	defer t.Close()
	u, e := t.GetTokenUser()
	if e != nil {
		return "", "", e
	}
	abs, e := filepath.Abs(config)
	if e != nil {
		return "", "", e
	}
	sid := u.User.Sid.String()
	return `\\.\pipe\new-api-reporter-` + Digest([]byte(sid + "|" + strings.ToLower(abs)))[:40], "D:P(A;;GA;;;" + sid + ")", nil
}
func listenControl(config string) (net.Listener, error) {
	address, acl, e := pipeIdentity(config)
	if e != nil {
		return nil, e
	}
	return winio.ListenPipe(address, &winio.PipeConfig{SecurityDescriptor: acl, InputBufferSize: 4096, OutputBufferSize: 4096})
}
func dialControl(ctx context.Context, config string) (net.Conn, error) {
	address, _, e := pipeIdentity(config)
	if e != nil {
		return nil, e
	}
	return winio.DialPipeContext(ctx, address)
}
