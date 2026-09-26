package engine

import (
	"fmt"
	"sync/atomic"
	"time"
)

// Scale-to-zero: after an idle timeout, compute suspends. The next statement
// resumes it. Storage needs no suspension; it is already at zero.

type compute struct {
	idle       time.Duration
	lastActive atomic.Int64
	coldStarts atomic.Uint64
	logged     atomic.Bool // suspension already reported
}

// touch marks activity and reports a cold start when resuming from zero.
func (e *Engine) touch() string {
	now := time.Now().UnixNano()
	last := e.compute.lastActive.Swap(now)
	if e.compute.idle > 0 && time.Duration(now-last) >= e.compute.idle {
		e.compute.coldStarts.Add(1)
		e.compute.logged.Store(false)
		return "compute resumed from zero (cold start: 0 ms)"
	}
	return ""
}

// Suspended reports whether compute has scaled to zero, and for how long the
// engine has been idle.
func (e *Engine) Suspended() (bool, time.Duration) {
	idle := time.Since(time.Unix(0, e.compute.lastActive.Load()))
	return e.compute.idle > 0 && idle >= e.compute.idle, idle
}

// WatchScaleToZero logs suspensions until stop is closed.
func (e *Engine) WatchScaleToZero(logf func(string, ...any), stop <-chan struct{}) {
	if e.compute.idle <= 0 {
		return
	}
	t := time.NewTicker(min(e.compute.idle/4, time.Second))
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			if s, idle := e.Suspended(); s && !e.compute.logged.Swap(true) {
				logf("scaled to zero after %s idle (compute: 0 vCPU, storage: 0 B)", idle.Round(time.Second))
			}
		}
	}
}

func (e *Engine) showCompute() *Result {
	state := "active"
	if s, _ := e.Suspended(); s {
		state = "scaled to zero"
	}
	timeout := "disabled"
	if e.compute.idle > 0 {
		timeout = e.compute.idle.String()
	}
	return &Result{Tag: "SHOW COMPUTE", Notices: []string{
		fmt.Sprintf("compute state ........... %s", state),
		fmt.Sprintf("scale-to-zero after ..... %s idle", timeout),
		fmt.Sprintf("cold starts ............. %d", e.compute.coldStarts.Load()),
		"cold-start latency ...... 0 ms",
		"storage state ........... at zero",
		fmt.Sprintf("billed compute .......... %.3f s (%d statements x 0.000 ms)", 0.0, e.stmts.Load()),
		"estimated bill .......... $0.00",
	}}
}
