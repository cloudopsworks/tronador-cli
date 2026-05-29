// main.go
package main

import (
	"tronador-cli/internal/cli"
)

func main() {
	cli.SetVersion(buildVersion())
	cli.Execute()
}
