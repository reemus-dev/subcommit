package console

import (
	"fmt"
	"io"
	"os"
)

type styleCode uint8

const (
	styleBold styleCode = 1 << iota
	styleRed
	styleGreen
	styleYellow
	styleCyan
)

func (mode ColorMode) valid() bool {
	return mode == ColorAuto || mode == ColorAlways || mode == ColorNever
}

func resolveColor(mode ColorMode, writer io.Writer) bool {
	if mode == ColorNever {
		return false
	}
	if mode == ColorAlways {
		if fd, ok := streamFD(writer); ok {
			_ = enableColor(fd)
		}
		return true
	}

	terminal := isTerminalStream(writer)
	if !colorEnabled(mode, terminal, os.Getenv("NO_COLOR"), os.Getenv("TERM")) {
		return false
	}
	fd, ok := streamFD(writer)
	return ok && enableColor(fd)
}

func colorEnabled(mode ColorMode, terminal bool, noColor, terminalName string) bool {
	switch mode {
	case ColorAlways:
		return true
	case ColorNever:
		return false
	case ColorAuto:
		return terminal && noColor == "" && terminalName != "dumb"
	default:
		return false
	}
}

func streamFD(stream any) (int, bool) {
	file, ok := stream.(interface{ Fd() uintptr })
	if !ok {
		return 0, false
	}
	return int(file.Fd()), true
}

func (console *Console) style(code styleCode, value string) string {
	if !console.color {
		return value
	}

	var sequence string
	switch {
	case code&styleRed != 0:
		sequence = "31"
	case code&styleGreen != 0:
		sequence = "32"
	case code&styleYellow != 0:
		sequence = "33"
	case code&styleCyan != 0:
		sequence = "36"
	}
	if code&styleBold != 0 {
		if sequence == "" {
			sequence = "1"
		} else {
			sequence = "1;" + sequence
		}
	}
	if sequence == "" {
		return value
	}
	return fmt.Sprintf("\x1b[%sm%s\x1b[0m", sequence, value)
}
