//go:build !darwin

package jsc

func newContext() (uintptr, error)           { return 0, ErrUnsupported }
func releaseContext(uintptr)                 {}
func evalIn(uintptr, string) (string, error) { return "", ErrUnsupported }
