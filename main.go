// main.go 负责服务入口与启动参数委派。
package main

import (
	"os"

	"forward-stub/src/bootstrap"

	_ "go.uber.org/automaxprocs/maxprocs"
)

var version = "dev"

func main() {
	os.Exit(bootstrap.Run(version, os.Args[1:]))
}
