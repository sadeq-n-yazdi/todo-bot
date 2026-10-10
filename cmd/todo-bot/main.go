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
	"fmt"
	"os"

	_ "time/tzdata" // embed the time zone database; the host may lack it
)

// version is set with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	cmd := "run"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "version":
		fmt.Println(version)
	case "run", "migrate", "backup":
		fmt.Fprintf(os.Stderr, "todo-bot %s: %q is not implemented yet (#13)\n", version, cmd)
		os.Exit(1)
	default:
		fmt.Fprintf(os.Stderr, "usage: todo-bot [run|version|migrate|backup PATH]\n")
		os.Exit(2)
	}
}
