// Command th-cli is a thin, agent-friendly CLI over trendHERO's public API.
package main

import (
	"os"

	"github.com/vnazarenko/th-cli/cmd"
)

func main() {
	os.Exit(cmd.Execute())
}
