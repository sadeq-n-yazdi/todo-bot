package web

import (
	"io/fs"
	"log/slog"

	"github.com/sadeq-n-yazdi/todo-bot/internal/clock"
	"github.com/sadeq-n-yazdi/todo-bot/internal/config"
	"github.com/sadeq-n-yazdi/todo-bot/internal/nlparse"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

// Deps is everything the web server needs (docs/WEB-DESIGN.md §13).
type Deps struct {
	Svc     service.Service
	Web     service.Web
	Send    tg.Sender
	Members tg.MemberChecker
	Parser  nlparse.Parser
	Clock   clock.Clock
	Allowed map[int64]bool
	Token   string // bot token, for the HMAC checks only; never logged
	BotName string // the bot @username, for t.me links
	Config  config.Config
	Assets  fs.FS
	Log     *slog.Logger
}
