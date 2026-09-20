package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"example.com/solo-0009-archive-weave/internal/workflowcheck"
)

func main() {
	workflow := flag.String("workflow", "all", "workflow check to run")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := workflowcheck.Run(ctx, *workflow); err != nil {
		fmt.Fprintf(os.Stderr, "workflow check failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("workflow check passed: %s\n", *workflow)
}
