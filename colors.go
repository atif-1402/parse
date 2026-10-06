package main

import "strings"

// ANSI escape codes.
const (
	reset  = "\033[0m"
	bold   = "\033[1m"
	dim    = "\033[2m"
	red    = "\033[31m"
	green  = "\033[32m"
	yellow = "\033[33m"
	cyan   = "\033[36m"
)

var colorCodes = map[string]string{
	"bold":   bold,
	"dim":    dim,
	"red":    red,
	"green":  green,
	"yellow": yellow,
	"cyan":   cyan,
}

// colorEnabled is set once at startup (see shouldColor in main.go).
var colorEnabled bool

// paint wraps s in the named color(s), e.g. "green" or "bold red". It returns
// s unchanged when color is off, the name is unknown, or s is empty.
func paint(color, s string) string {
	if !colorEnabled || color == "" || s == "" {
		return s
	}
	codes := ""
	for _, name := range strings.Fields(color) {
		codes += colorCodes[name]
	}
	if codes == "" {
		return s
	}
	return codes + s + reset
}
