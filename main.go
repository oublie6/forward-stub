// main.go 负责服务入口与启动参数委派。
package main

import (
	"log"
	"os"
	"runtime"

	"forward-stub/src/bootstrap"
	"go.uber.org/automaxprocs/maxprocs"
)

func main() {
	if _, err := maxprocs.Set(); err != nil {
		log.Printf("automaxprocs apply failed: %v", err)
	} else {
		log.Printf("automaxprocs applied, GOMAXPROCS=%d", runtime.GOMAXPROCS(0))
	}

	os.Exit(bootstrap.Run(os.Args[1:]))
}
