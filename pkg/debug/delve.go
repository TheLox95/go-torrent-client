package delve

import (
	"os"
	"strings"
)

func RunningWithDelve() bool {
	argsWithProg := os.Args

	if strings.Contains(argsWithProg[0], "debug") {
		return true
	} else {
		return false
	}
}
