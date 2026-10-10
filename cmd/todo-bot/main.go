// Command todo-bot is a self-hosted Telegram todo bot with reminders.
//
// Usage:
//
//	todo-bot [run]        start the bot (default)
//	todo-bot version      print the version
//	todo-bot migrate      apply database migrations and exit
//	todo-bot backup PATH  write a consistent database copy to PATH
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	_ "time/tzdata" // embed the time zone database; the host may lack it
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// Once the first signal arrived, restore the default handling so that a
	// second SIGTERM or Ctrl-C kills the process during a slow shutdown.
	go func() { <-ctx.Done(); stop() }()
	code := run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
