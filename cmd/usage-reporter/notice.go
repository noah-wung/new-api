package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/QuantumNous/new-api/pkg/reporterupdate"
	"github.com/QuantumNous/new-api/pkg/usagereporter"
)

func checkCollectorVersion(ctx context.Context, cfg usagereporter.Config) error {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	n := reporterupdate.NoticeChecker{
		Root:   filepath.Join(home, ".new-api-usage-reporter", "notices"),
		Server: cfg.Server, Source: cfg.Source, Credential: cfg.Credential,
		Target: runtime.GOOS + "-" + runtime.GOARCH,
	}
	return n.Check(ctx, os.Stdout)
}

func runClientWithNotices(ctx context.Context, cfg usagereporter.Config, streaming bool) error {
	if !streaming {
		// Polling delivery always runs before the advisory network request.
		err := usagereporter.RunClient(ctx, cfg, false, os.Stdout, os.Stderr)
		noticeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		if e := checkCollectorVersion(noticeCtx, cfg); e != nil {
			fmt.Fprintln(os.Stderr, "Collector version check:", reporterupdate.ErrorCode(e))
		}
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()
		for {
			noticeCtx, stop := context.WithTimeout(ctx, 20*time.Second)
			err := checkCollectorVersion(noticeCtx, cfg)
			stop()
			if err != nil && ctx.Err() == nil {
				fmt.Fprintln(os.Stderr, "Collector version check:", reporterupdate.ErrorCode(err))
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	err := usagereporter.RunClient(ctx, cfg, true, os.Stdout, os.Stderr)
	cancel()
	<-done
	return err
}
