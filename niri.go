package main

import (
	"bufio"
	"context"
	"encoding/json"
	"os/exec"
	"sync/atomic"
	"time"
)

// watchNiri wakes the scheduler on window events and reconnects after IPC loss.
func watchNiri(ctx context.Context, wake chan<- struct{}, disconnects *atomic.Uint64, connected *atomic.Bool) {
	for ctx.Err() == nil {
		cmd := exec.CommandContext(ctx, "niri", "msg", "--json", "event-stream")
		out, err := cmd.StdoutPipe()
		if err == nil {
			err = cmd.Start()
		}
		if err == nil {
			connected.Store(true)
			scanner := bufio.NewScanner(out)
			scanner.Buffer(make([]byte, 4096), 8<<20)
			for scanner.Scan() {
				var event map[string]json.RawMessage
				if json.Unmarshal(scanner.Bytes(), &event) != nil {
					continue
				}
				if _, ok := event["WindowFocusChanged"]; ok {
					select {
					case wake <- struct{}{}:
					default:
					}
				}
				if _, ok := event["WindowsChanged"]; ok {
					select {
					case wake <- struct{}{}:
					default:
					}
				}
				if _, ok := event["WindowClosed"]; ok {
					select {
					case wake <- struct{}{}:
					default:
					}
				}
			}
			connected.Store(false)
			disconnects.Add(1)
			_ = cmd.Wait()
			select {
			case wake <- struct{}{}:
			default:
			}
		}
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
