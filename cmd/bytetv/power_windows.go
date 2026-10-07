//go:build windows

package main

import (
	"runtime"
	"sync"
	"syscall"
)

const (
	esContinuous     = 0x80000000
	esSystemRequired = 0x00000001
)

var setThreadExecutionState = syscall.NewLazyDLL("kernel32.dll").NewProc("SetThreadExecutionState")

// powerAssertion：SetThreadExecutionState 按线程生效，因此用一个锁定 OS 线程的专用 goroutine 持有。
type powerAssertion struct {
	mu   sync.Mutex
	stop chan struct{}
}

func (p *powerAssertion) acquire(string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stop != nil {
		return
	}
	p.stop = make(chan struct{})
	go func(stop chan struct{}) {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		setThreadExecutionState.Call(uintptr(esContinuous | esSystemRequired))
		<-stop
		setThreadExecutionState.Call(uintptr(esContinuous))
	}(p.stop)
}

func (p *powerAssertion) release() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stop != nil {
		close(p.stop)
		p.stop = nil
	}
}
