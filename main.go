// main.go 负责服务入口与启动参数委派。
package main

import (
	"os"

	"forward-stub/src/bootstrap"

	_ "go.uber.org/automaxprocs/maxprocs"
)

var version = "dev"

// main 从命令行入口启动 forward-stub 进程。
func main() {
	os.Exit(bootstrap.Run(version, os.Args[1:]))
}
