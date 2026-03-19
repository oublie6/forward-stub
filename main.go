// main.go 负责服务入口与启动参数委派。
package main

import (
	"os"

	"forward-stub/src/bootstrap"

	_ "go.uber.org/automaxprocs/maxprocs"
)

func main() {
	os.Exit(bootstrap.Run(os.Args[1:]))
}
