package jsc

import (
	"runtime"
	"strings"
	"testing"
)

// A script's value comes back as a string, and what it throws as an
// error, with text beyond ASCII intact both ways.
func TestEval(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("JavaScriptCore is macOS's")
	}
	got, err := Eval(`JSON.stringify({s: "歌" + [1, 2].map(x => x * 2).join("")})`)
	if err != nil || got != `{"s":"歌24"}` {
		t.Fatalf("%q, %v", got, err)
	}
	if _, err := Eval(`throw new Error("没有")`); err == nil || !strings.Contains(err.Error(), "没有") {
		t.Fatalf("%v", err)
	}
}

// A context kept sees what the scripts before left in it.
func TestContextKeeps(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("JavaScriptCore is macOS's")
	}
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Eval(`var twice = s => s + s`); err != nil {
		t.Fatal(err)
	}
	if got, err := c.Eval(`twice("ab")`); err != nil || got != "abab" {
		t.Fatalf("%q, %v", got, err)
	}
}
