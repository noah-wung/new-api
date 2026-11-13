package helper

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/bytedance/gopkg/util/gopool"

	"github.com/gin-gonic/gin"
)

const (
	InitialScannerBufferSize    = 64 << 10  // 64KB (64*1024)
	DefaultMaxScannerBufferSize = 128 << 20 // 64MB (64*1024*1024) default SSE buffer size
	DefaultPingInterval         = 10 * time.Second
	// streamNoDataCheckInterval is how often the no-data watchdog re-checks the
	// last-real-data timestamp. It only bounds the detection latency of the
	// no-data cap (detection = cap + interval at worst); keep it small.
	streamNoDataCheckInterval = 1 * time.Second
	// streamWriteTimeout bounds a single blocked write to a slow client so the
	// unconditional wg.Wait() in cleanup can always finish. Without it, a slow
	// but connected client (full TCP buffer, no server WriteTimeout) could hang
	// the handler forever.
	streamWriteTimeout = 30 * time.Second
)

func getScannerBufferSize() int {
	if constant.StreamScannerMaxBufferMB > 0 {
		return constant.StreamScannerMaxBufferMB << 20
	}
	return DefaultMaxScannerBufferSize
}

func NewStreamScanner(reader io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, InitialScannerBufferSize), getScannerBufferSize())
	return scanner
}

// ExtendWriteDeadline pushes the connection write deadline forward before each
// stream write. Best-effort: writers that don't support deadlines (e.g.
// httptest recorders) are silently ignored.
func ExtendWriteDeadline(c *gin.Context) {
	if c == nil || c.Writer == nil {
		return
	}
	_ = http.NewResponseController(c.Writer).SetWriteDeadline(time.Now().Add(streamWriteTimeout))
}

func StreamScannerHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, dataHandler func(data string, sr *StreamResult)) {

	if resp == nil || dataHandler == nil {
		return
	}

	// 无条件新建 StreamStatus
	info.StreamStatus = relaycommon.NewStreamStatus()

	ctx, cancel := context.WithCancel(context.Background())

	streamingTimeout := time.Duration(constant.StreamingTimeout) * time.Second

	var (
		stopChan    = make(chan bool, 3) // 增加缓冲区避免阻塞
		scanner     = NewStreamScanner(resp.Body)
		ticker      = time.NewTicker(streamingTimeout)
		pingTicker  *time.Ticker
		writeMutex  sync.Mutex     // Mutex to protect concurrent writes
		wg          sync.WaitGroup // 用于等待所有 goroutine 退出
		cleanupOnce sync.Once
		stopOnce    sync.Once
		// lastRealData tracks the last time a forwardable "data:" payload line
		// arrived. Keepalive/comment lines refresh the idle ticker (pipe alive)
		// but deliberately do NOT refresh this: a stuck generation must still
		// hit the no-data cap. Atomic because scanner goroutine writes while
		// the no-data watchdog reads.
		lastRealData atomic.Int64
	)
	lastRealData.Store(time.Now().UnixNano())
	// 无数据上限来自 general_setting（管理后台可热改）；0 表示禁用看门狗。
	noDataCap := time.Duration(operation_setting.GetGeneralSetting().StreamNoDataTimeoutSeconds) * time.Second

	stop := func() {
		stopOnce.Do(func() {
			close(stopChan)
		})
	}

	generalSettings := operation_setting.GetGeneralSetting()
	pingEnabled := generalSettings.PingIntervalEnabled && !info.DisablePing
	pingInterval := time.Duration(generalSettings.PingIntervalSeconds) * time.Second
	if pingInterval <= 0 {
		pingInterval = DefaultPingInterval
	}

	if pingEnabled {
		pingTicker = time.NewTicker(pingInterval)
	}

	logger.LogDebug(c, "relay timeout seconds: %d", common.RelayTimeout)
	logger.LogDebug(c, "relay max idle conns: %d", common.RelayMaxIdleConns)
	logger.LogDebug(c, "relay max idle conns per host: %d", common.RelayMaxIdleConnsPerHost)
	logger.LogDebug(c, "streaming timeout seconds: %d", int64(streamingTimeout.Seconds()))
	logger.LogDebug(c, "ping interval seconds: %d", int64(pingInterval.Seconds()))

	cleanup := func() {
		cleanupOnce.Do(func() {
			cancel()
			stop()
			if resp.Body != nil {
				_ = resp.Body.Close()
			}

			ticker.Stop()
			if pingTicker != nil {
				pingTicker.Stop()
			}

			wg.Wait()
		})
	}
	// Ensure gin.Context is not returned to Gin's pool while any stream goroutine can still use it.
	defer cleanup()

	scanner.Split(bufio.ScanLines)
	SetEventStreamHeaders(c)

	ctx = context.WithValue(ctx, "stop_chan", stopChan)

	// Handle ping data sending with improved error handling
	if pingEnabled && pingTicker != nil {
		wg.Add(1)
		gopool.Go(func() {
			defer func() {
				if r := recover(); r != nil {
					logger.LogError(c, fmt.Sprintf("ping goroutine panic: %v", r))
					info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPanic, fmt.Errorf("ping panic: %v", r))
					stop()
				}
				logger.LogDebug(c, "ping goroutine exited")
				wg.Done()
			}()

			// 添加超时保护，防止 goroutine 无限运行
			maxPingDuration := 30 * time.Minute // 最大 ping 持续时间
			pingTimeout := time.NewTimer(maxPingDuration)
			defer pingTimeout.Stop()

			for {
				select {
				case <-pingTicker.C:
					var err error
					func() {
						writeMutex.Lock()
						defer writeMutex.Unlock()
						ExtendWriteDeadline(c)
						err = PingData(c)
					}()
					if err != nil {
						logger.LogError(c, "ping data error: "+err.Error())
						info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPingFail, err)
						return
					}
					logger.LogDebug(c, "ping data sent")
				case <-ctx.Done():
					return
				case <-stopChan:
					return
				case <-c.Request.Context().Done():
					// 监听客户端断开连接
					return
				case <-pingTimeout.C:
					logger.LogError(c, "ping goroutine max duration reached")
					return
				}
			}
		})
	}

	dataChan := make(chan string, 10)

	wg.Add(1)
	gopool.Go(func() {
		defer func() {
			if r := recover(); r != nil {
				logger.LogError(c, fmt.Sprintf("data handler goroutine panic: %v", r))
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPanic, fmt.Errorf("handler panic: %v", r))
			}
			stop()
			wg.Done()
		}()
		sr := newStreamResult(info.StreamStatus)
		for data := range dataChan {
			sr.reset()
			func() {
				writeMutex.Lock()
				defer writeMutex.Unlock()
				ExtendWriteDeadline(c)
				dataHandler(data, sr)
			}()
			if sr.IsStopped() {
				return
			}
		}
	})

	// Scanner goroutine with improved error handling
	wg.Add(1)
	common.RelayCtxGo(ctx, func() {
		defer func() {
			close(dataChan)
			if r := recover(); r != nil {
				logger.LogError(c, fmt.Sprintf("scanner goroutine panic: %v", r))
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPanic, fmt.Errorf("scanner panic: %v", r))
			}
			stop()
			logger.LogDebug(c, "scanner goroutine exited")
			wg.Done()
		}()

		for scanner.Scan() {
			// 检查是否需要停止
			select {
			case <-stopChan:
				return
			case <-ctx.Done():
				return
			default:
			}

			ticker.Reset(streamingTimeout)
			data := scanner.Text()
			logger.LogDebug(c, "stream scanner data: %s", data)

			if len(data) < 6 {
				continue
			}
			if data[:5] != "data:" && data[:6] != "[DONE]" {
				continue
			}
			data = data[5:]
			data = strings.TrimSpace(data)
			if data == "" {
				continue
			}
			if !strings.HasPrefix(data, "[DONE]") {
				info.SetFirstResponseTime()
				lastRealData.Store(time.Now().UnixNano())
				info.ReceivedResponseCount++

				select {
				case dataChan <- data:
				case <-ctx.Done():
					return
				case <-stopChan:
					return
				}
			} else {
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
				logger.LogDebug(c, "received [DONE], stopping scanner")
				return
			}
		}

		if err := scanner.Err(); err != nil {
			if err != io.EOF {
				logger.LogError(c, "scanner error: "+err.Error())
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, err)
			}
		}
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonEOF, nil)
	})

	// 无数据看门狗：上面的空闲 ticker 会被任意上游行（含保活行）刷新——保活行
	// 证明管道活着，但不代表在生成。上游黑洞（接收请求、只回保活、永不产出）时
	// 该 ticker 永远不会触发，流会一直挂到客户端放弃或上游自方的小时级超时。
	// 此看门狗要求在 noDataCap 内必须出现过可转发的 "data:" 载荷行，否则主动
	// 断流，让客户端拿到明确错误并及时重试。
	if noDataCap > 0 {
		wg.Add(1)
		gopool.Go(func() {
			defer wg.Done()
			checkTicker := time.NewTicker(streamNoDataCheckInterval)
			defer checkTicker.Stop()
			for {
				select {
				case <-checkTicker.C:
					if time.Since(time.Unix(0, lastRealData.Load())) >= noDataCap {
						info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonNoDataTimeout,
							fmt.Errorf("no forwardable data within %s", noDataCap))
						stop()
						return
					}
				case <-ctx.Done():
					return
				case <-stopChan:
					return
				}
			}
		})
	}

	// 主循环等待完成或超时
	select {
	case <-ticker.C:
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonTimeout, nil)
	case <-stopChan:
		// EndReason already set by the goroutine that triggered stopChan
	case <-c.Request.Context().Done():
		// 客户端断开：立即 cleanup 关闭上游 resp.Body，解除 scanner 阻塞并让上游停止生成，
		// 避免为已放弃的请求继续消费上游 token。
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, c.Request.Context().Err())
	}

	cleanup()
	// 客户端还连着时（no_data_timeout 正是这种场景），补发一条 SSE 错误事件，
	// 让客户端立刻感知失败并可自动重试，而不是看到流静默结束。
	if info.StreamStatus.EndReason == relaycommon.StreamEndReasonNoDataTimeout ||
		info.StreamStatus.EndReason == relaycommon.StreamEndReasonTimeout {
		writeMutex.Lock()
		ExtendWriteDeadline(c)
		errPayload, marshalErr := common.Marshal(gin.H{"error": gin.H{
			"message": fmt.Sprintf("stream aborted without upstream data (reason=%s)",
				info.StreamStatus.EndReason),
			"type": "new_api_error",
			"code": string(info.StreamStatus.EndReason),
		}})
		writeMutex.Unlock()
		if marshalErr == nil {
			_ = StringData(c, string(errPayload))
		}
	}
	if info.StreamStatus.IsNormalEnd() && !info.StreamStatus.HasErrors() {
		logger.LogInfo(c, fmt.Sprintf("stream ended: %s", info.StreamStatus.Summary()))
	} else {
		logger.LogError(c, fmt.Sprintf("stream ended: %s, received=%d", info.StreamStatus.Summary(), info.ReceivedResponseCount))
	}
}
