//go:build darwin

package main

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include <IOKit/pwr_mgt/IOPMLib.h>
#include <stdlib.h>

static IOPMAssertionID bytetvAssert(const char *reason) {
	IOPMAssertionID id = 0;
	CFStringRef name = CFStringCreateWithCString(kCFAllocatorDefault, reason, kCFStringEncodingUTF8);
	IOReturn ret = IOPMAssertionCreateWithName(kIOPMAssertionTypePreventUserIdleSystemSleep, kIOPMAssertionLevelOn, name, &id);
	CFRelease(name);
	return ret == kIOReturnSuccess ? id : 0;
}
static void bytetvRelease(IOPMAssertionID id) { if (id) IOPMAssertionRelease(id); }
*/
import "C"

import (
	"sync"
	"unsafe"
)

// powerAssertion 下载/合并期间阻止系统空闲睡眠（允许屏幕关闭），对应 Electron powerSaveBlocker("prevent-app-suspension")。
type powerAssertion struct {
	mu sync.Mutex
	id C.IOPMAssertionID
}

func (p *powerAssertion) acquire(reason string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.id != 0 {
		return
	}
	cs := C.CString(reason)
	defer C.free(unsafe.Pointer(cs))
	p.id = C.bytetvAssert(cs)
}

func (p *powerAssertion) release() {
	p.mu.Lock()
	defer p.mu.Unlock()
	C.bytetvRelease(p.id)
	p.id = 0
}
