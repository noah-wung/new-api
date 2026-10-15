package reporterupdate

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const controlTimeout = 500 * time.Millisecond

type Health struct {
	PID         int    `json:"pid"`
	ReleaseID   string `json:"release_id"`
	Source      string `json:"source"`
	Transaction string `json:"transaction"`
	Ready       bool   `json:"ready"`
}

// ServeControl owns a profile's exclusive local endpoint until its worker exits.
func ServeControl(ctx context.Context, config string, initial Health, stop context.CancelFunc) (func(), func(), error) {
	l, err := listenControl(config)
	if err != nil {
		return nil, nil, err
	}
	var mu sync.Mutex
	ready := func() { mu.Lock(); initial.Ready = true; mu.Unlock() }
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
		case <-done:
		}
		_ = l.Close()
	}()
	go func() {
		for {
			conn, e := l.Accept()
			if e != nil {
				return
			}
			go handleControl(conn, &mu, &initial, stop)
		}
	}()
	var once sync.Once
	closeControl := func() { once.Do(func() { _ = l.Close(); close(done) }) }
	return ready, closeControl, nil
}

func handleControl(conn net.Conn, mu *sync.Mutex, health *Health, stop context.CancelFunc) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	var request struct {
		Action string `json:"action"`
	}
	if common.DecodeJson(conn, &request) != nil {
		return
	}
	if request.Action != "health" && request.Action != "stop" {
		return
	}
	mu.Lock()
	data, err := common.Marshal(*health)
	mu.Unlock()
	if err != nil {
		return
	}
	_, _ = conn.Write(append(data, '\n'))
	if request.Action == "stop" {
		stop()
	}
}

func QueryControl(ctx context.Context, config, action string) (Health, error) {
	var health Health
	if action != "health" && action != "stop" {
		return health, errors.New("invalid_control_action")
	}
	conn, err := dialControl(ctx, config)
	if err != nil {
		return health, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	data, err := common.Marshal(map[string]string{"action": action})
	if err != nil {
		return health, err
	}
	if _, err = conn.Write(append(data, '\n')); err != nil {
		return health, err
	}
	err = common.DecodeJson(conn, &health)
	return health, err
}
