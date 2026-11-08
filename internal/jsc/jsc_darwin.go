//go:build darwin

package jsc

import (
	"errors"
	"runtime"
	"sync"

	"github.com/ebitengine/purego"
)

var (
	loadOnce sync.Once
	loadErr  error

	jsGlobalContextCreate      func(class uintptr) uintptr
	jsGlobalContextRelease     func(ctx uintptr)
	jsStringCreateWithUTF8CStr func(s *byte) uintptr
	jsStringRelease            func(s uintptr)
	jsStringGetMaximumUTF8Size func(s uintptr) uintptr
	jsStringGetUTF8CString     func(s uintptr, buf *byte, size uintptr) uintptr
	jsEvaluateScript           func(ctx, script, this, url uintptr, line int32, exc *uintptr) uintptr
	jsValueToStringCopy        func(ctx, v uintptr, exc *uintptr) uintptr
)

func load() error {
	loadOnce.Do(func() {
		lib, err := purego.Dlopen("/System/Library/Frameworks/JavaScriptCore.framework/JavaScriptCore", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			loadErr = err
			return
		}
		purego.RegisterLibFunc(&jsGlobalContextCreate, lib, "JSGlobalContextCreate")
		purego.RegisterLibFunc(&jsGlobalContextRelease, lib, "JSGlobalContextRelease")
		purego.RegisterLibFunc(&jsStringCreateWithUTF8CStr, lib, "JSStringCreateWithUTF8CString")
		purego.RegisterLibFunc(&jsStringRelease, lib, "JSStringRelease")
		purego.RegisterLibFunc(&jsStringGetMaximumUTF8Size, lib, "JSStringGetMaximumUTF8CStringSize")
		purego.RegisterLibFunc(&jsStringGetUTF8CString, lib, "JSStringGetUTF8CString")
		purego.RegisterLibFunc(&jsEvaluateScript, lib, "JSEvaluateScript")
		purego.RegisterLibFunc(&jsValueToStringCopy, lib, "JSValueToStringCopy")
	})
	return loadErr
}

// jsString makes a JSStringRef of s, which the caller releases.
func jsString(s string) uintptr {
	b := make([]byte, len(s)+1)
	copy(b, s)
	r := jsStringCreateWithUTF8CStr(&b[0])
	runtime.KeepAlive(b)
	return r
}

// goString reads a JSStringRef.
func goString(s uintptr) string {
	n := jsStringGetMaximumUTF8Size(s)
	if n == 0 {
		return ""
	}
	buf := make([]byte, n)
	w := jsStringGetUTF8CString(s, &buf[0], n)
	if w == 0 {
		return ""
	}
	return string(buf[:w-1]) // without its NUL
}

func newContext() (uintptr, error) {
	if err := load(); err != nil {
		return 0, errors.Join(ErrUnsupported, err)
	}
	return jsGlobalContextCreate(0), nil
}

func releaseContext(ctx uintptr) { jsGlobalContextRelease(ctx) }

func evalIn(ctx uintptr, script string) (string, error) {
	// A context may be used from any thread, one at a time: a goroutine
	// keeps to its thread while it has one.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	src := jsString(script)
	defer jsStringRelease(src)
	var exc uintptr
	v := jsEvaluateScript(ctx, src, 0, 0, 1, &exc)
	if exc != 0 {
		return "", errors.New("jsc: " + valueString(ctx, exc))
	}
	return valueString(ctx, v), nil
}

// valueString is a value as JavaScript's String() makes it.
func valueString(ctx, v uintptr) string {
	if v == 0 {
		return ""
	}
	var exc uintptr
	s := jsValueToStringCopy(ctx, v, &exc)
	if s == 0 {
		return ""
	}
	defer jsStringRelease(s)
	return goString(s)
}
