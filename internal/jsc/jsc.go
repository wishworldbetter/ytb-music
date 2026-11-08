// Package jsc runs JavaScript in the system's JavaScriptCore, on macOS,
// through its C API: a bare context, with the language's own objects and
// nothing of a browser's or a file system's, for scripts that only compute.
package jsc

import (
	"errors"
	"sync"
)

// ErrUnsupported is what anywhere without JavaScriptCore gives.
var ErrUnsupported = errors.New("jsc: JavaScriptCore is only on macOS")

// Context is a context kept for scripts run one after another, each
// seeing what those before it left. It runs one at a time.
type Context struct {
	mu  sync.Mutex
	ref uintptr
}

// New returns a context of its own.
func New() (*Context, error) {
	ref, err := newContext()
	if err != nil {
		return nil, err
	}
	return &Context{ref: ref}, nil
}

// Eval runs script and returns its value as a string, or what it threw.
func (c *Context) Eval(script string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ref == 0 {
		return "", errors.New("jsc: the context is closed")
	}
	return evalIn(c.ref, script)
}

// Close lets the context go.
func (c *Context) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ref != 0 {
		releaseContext(c.ref)
		c.ref = 0
	}
}

// Eval runs script in a context of its own and returns its value as a
// string, or what it threw.
func Eval(script string) (string, error) {
	c, err := New()
	if err != nil {
		return "", err
	}
	defer c.Close()
	return c.Eval(script)
}
