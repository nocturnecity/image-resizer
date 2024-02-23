package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nocturnecity/image-resizer/internal"
)

const runCmd = "run"
const defaultPort = 8080
const defaultTimeout = 90
const defaultMemoryLimit = 250
const defaultLogLvl = "info"

func main() {
	flag.Parse()

	if len(os.Args[1:]) < 1 {
		fmt.Printf("resizer: one of the following command expected: '%v'\n", []string{runCmd})
		os.Exit(1)
	}
	cmdName := os.Args[1]
	args := os.Args[2:]
	var (
		logLVL           string
		port             int
		timeout          int
		memoryLimit      int
		workingDirectory string
	)

	cmd := flag.NewFlagSet(runCmd, flag.ExitOnError)
	cmd.StringVar(&logLVL, "loglvl", defaultLogLvl, "set logging level: 'debug', 'info', 'error'")
	cmd.StringVar(&workingDirectory, "working-directory", "", "set directory to save temporary files")
	cmd.IntVar(&memoryLimit, "memory-limit", defaultMemoryLimit, "set MB memory limit per command")
	cmd.IntVar(&port, "port", defaultPort, "set HTTP server port")
	cmd.IntVar(&timeout, "timeout", defaultTimeout, "set HTTP server timeout seconds")

	if err := cmd.Parse(args); err != nil {
		fmt.Printf("resizer: error parsing arguments: '%v'\n", err)
		os.Exit(1)
	}

	lvl, lvlErr := internal.ParseLevel(logLVL)
	if lvlErr != nil {
		fmt.Printf("resizer: error parsing log level: '%v'\n", lvlErr)
		os.Exit(1)
	}

	stdLog := internal.NewStdLog(internal.WithLevel(lvl))
	ctx := context.Background()
	var server *internal.Server
	switch cmdName {
	case runCmd:
		server = internal.NewHttpServer(
			port,
			time.Duration(timeout)*time.Second,
			memoryLimit,
			workingDirectory,
			stdLog)
	default:
		stdLog.Fatal("Unknown sub-command: %s\n", args[0])
	}

	go server.Run()
	defer server.Stop(ctx)
	// Wait for interrupt signal
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)

	// Wait for the interrupt signal or for both servers to finish
	select {
	case <-interrupt:
		stdLog.Info("Interrupt signal received, shutting down servers...")
	case <-ctx.Done():
		stdLog.Info("Context canceled, shutting down servers...")
	}
}
