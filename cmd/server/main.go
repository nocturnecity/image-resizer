package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/nocturnecity/image-resizer/internal"
	"os"
	"os/signal"
	"syscall"
)

const runCmd = "run"
const defaultPort = 8080

func main() {
	flag.Parse()

	if len(os.Args[1:]) < 1 {
		fmt.Printf("resizer: one of the following command expected: '%v'\n", []string{runCmd})
		os.Exit(1)
	}
	cmdName := os.Args[1]
	args := os.Args[2:]
	var (
		logLVL string
		port   int
	)

	cmd := flag.NewFlagSet(runCmd, flag.ExitOnError)
	cmd.StringVar(&logLVL, "loglvl", "debug", "set logging level: 'debug', 'info', 'error'")
	cmd.IntVar(&port, "port", defaultPort, "set HTTP server port")

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
		server = internal.NewHttpServer(port, stdLog)
	default:
		stdLog.Fatal("Unknown sub-command: %s\n", args[0])
	}

	go server.Run()
	// Wait for interrupt signal
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)

	// Wait for the interrupt signal or for both servers to finish
	select {
	case <-interrupt:
		fmt.Println("Interrupt signal received, shutting down servers...")
	case <-ctx.Done():
		fmt.Println("Context canceled, shutting down servers...")
	}
	server.Stop(ctx)
}
