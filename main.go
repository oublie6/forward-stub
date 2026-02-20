package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"forword-stub/src/app"
	"forword-stub/src/config"
	"forword-stub/src/control"
)

func main() {
	localPath := flag.String("config", "", "local config json path (optional)")
	apiURL := flag.String("api", "", "java config service url (optional)")
	timeoutSec := flag.Int("timeout", 5, "api timeout seconds")
	flag.Parse()

	var cfg config.Config
	var err error

	switch {
	case *localPath != "":
		cfg, err = config.LoadLocal(*localPath)
	case *apiURL != "":
		cli := control.NewConfigAPIClient(*apiURL, *timeoutSec)
		cfg, err = cli.FetchConfig(context.Background())
	default:
		fmt.Fprintln(os.Stderr, "must provide -config or -api")
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "load config error: %v\n", err)
		os.Exit(1)
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "config validate error: %v\n", err)
		os.Exit(1)
	}

	rt := app.NewRuntime()
	if err := rt.UpdateCache(context.Background(), cfg); err != nil {
		fmt.Fprintf(os.Stderr, "UpdateCache error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("forword-stub started. Press Ctrl+C to stop.")
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	_ = rt.Stop(context.Background())
	fmt.Println("forword-stub stopped.")
}
