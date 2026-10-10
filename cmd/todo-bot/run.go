package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"runtime/debug"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/clock"
	"github.com/sadeq-n-yazdi/todo-bot/internal/config"
	"github.com/sadeq-n-yazdi/todo-bot/internal/handler"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/lease"
	"github.com/sadeq-n-yazdi/todo-bot/internal/nlparse"
	"github.com/sadeq-n-yazdi/todo-bot/internal/scheduler"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
	_ "github.com/sadeq-n-yazdi/todo-bot/internal/store/sqlite" // registers the "sqlite" scheme
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg/tgbot"
)

// Set with -ldflags "-X main.version=... -X main.commit=... -X main.date=...".
// commit and date fall back to the VCS data Go embeds in the binary.
var (
	version = "dev"
	commit  = ""
	date    = ""
)

// shutdownTimeout bounds the whole shutdown (leader, scheduler, database).
var shutdownTimeout = 10 * time.Second

// tgServerURL points the Telegram client at another server; tests set it.
var tgServerURL string

// menu lists the bot commands shown in Telegram's menu, in order.
var menu = []string{"help", "add", "list", "done", "del", "edit", "today", "remind", "tz", "digest", "lang", "calendar"}

const usage = "usage: todo-bot [run|version|migrate|backup PATH]\n"

// run is the whole program: it returns the process exit code. args excludes
// the program name.
func run(ctx context.Context, args []string, env func(string) string, stdout, stderr io.Writer) int {
	cmd := "run"
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "version":
		_, _ = fmt.Fprintln(stdout, versionString())
		return 0
	case "run":
		return report(stderr, runBot(ctx, env, stderr))
	case "migrate":
		return report(stderr, migrate(ctx, env, stdout))
	case "backup":
		if len(args) != 2 {
			_, _ = fmt.Fprint(stderr, usage)
			return 2
		}
		return report(stderr, backup(ctx, env, args[1], stdout))
	default:
		_, _ = fmt.Fprint(stderr, usage)
		return 2
	}
}

func report(stderr io.Writer, err error) int {
	if err == nil {
		return 0
	}
	_, _ = fmt.Fprintf(stderr, "todo-bot: %v\n", err)
	return 1
}

func versionString() string {
	c, d := commit, date
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			switch {
			case s.Key == "vcs.revision" && c == "":
				c = s.Value
			case s.Key == "vcs.time" && d == "":
				d = s.Value
			}
		}
	}
	if c == "" {
		c = "unknown"
	}
	if d == "" {
		d = "unknown"
	}
	return fmt.Sprintf("todo-bot %s (commit %s, built %s)", version, c, d)
}

// loadAdminConfig loads the configuration for migrate and backup, which need
// only the database settings: the bot token and user list are filled with
// placeholders so that their absence is not an error.
func loadAdminConfig(env func(string) string) (config.Config, error) {
	return config.Load(func(k string) string {
		switch k {
		case "TELEGRAM_TOKEN":
			return "unused"
		case "TODO_ALLOWED_USERS":
			return "1"
		case "TODO_MODE":
			return config.ModePolling
		}
		return env(k)
	})
}

// openAdmin loads the config and opens the database for migrate and backup.
func openAdmin(ctx context.Context, env func(string) string) (store.Store, error) {
	cfg, err := loadAdminConfig(env)
	if err != nil {
		return nil, err
	}
	st, err := store.Open(ctx, cfg.DBURL)
	if err != nil {
		return nil, fmt.Errorf("open database %s: %w", redactURL(cfg.DBURL), err)
	}
	return st, nil
}

func migrate(ctx context.Context, env func(string) string, stdout io.Writer) error {
	st, err := openAdmin(ctx, env) // opening applies pending migrations
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	m, ok := st.(store.Migrator)
	if !ok {
		return errors.New("this database provider is not migrated by the bot")
	}
	if err := m.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	_, _ = fmt.Fprintln(stdout, "migrations applied")
	return nil
}

func backup(ctx context.Context, env func(string) string, dest string, stdout io.Writer) error {
	st, err := openAdmin(ctx, env)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	b, ok := st.(store.Backuper)
	if !ok {
		return errors.New("this database provider has no built-in backup; use its own dump tool")
	}
	if err := b.Backup(ctx, dest); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	_, _ = fmt.Fprintf(stdout, "backup written to %s\n", dest)
	return nil
}

// redactURL hides the password of a database URL. It never returns the raw
// string of a URL that does not parse.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "(invalid URL)"
	}
	return u.Redacted()
}

func runBot(ctx context.Context, env func(string) string, stderr io.Writer) error {
	cfg, err := config.Load(env)
	if err != nil {
		return err
	}
	if cfg.Mode == config.ModeWebhook {
		return errors.New("TODO_MODE=webhook: not supported until v0.2")
	}
	log := config.NewLogger(stderr, cfg.LogLevel)
	slog.SetDefault(log)
	log.Info("starting", "version", version, "node", cfg.NodeID, "db", redactURL(cfg.DBURL))

	st, err := store.Open(ctx, cfg.DBURL)
	if err != nil {
		return fmt.Errorf("open database %s: %w", redactURL(cfg.DBURL), err)
	}
	// The database is closed last, after the leader and the scheduler stopped.
	defer func() {
		if err := st.Close(); err != nil {
			log.Error("close database", "err", err)
		}
	}()

	bot, err := tgbot.New(ctx, cfg.Token, tgbot.Options{ServerURL: tgServerURL})
	if err != nil {
		return fmt.Errorf("telegram: %w", err)
	}

	clk := clock.Real{}
	svc := service.New(st, clk, service.Config{
		DefaultTZ:     cfg.DefaultTZ,
		DefaultHour:   cfg.DefaultHour,
		DefaultMinute: cfg.DefaultMinute,
		NodeID:        cfg.NodeID,
		ClaimTTL:      cfg.ClaimTTL,
	})
	svc.SetLogger(log)
	sched := scheduler.New(svc, bot, clk, log,
		scheduler.WithMaxSleep(cfg.SchedMaxSleep), scheduler.WithClaimTTL(cfg.ClaimTTL))
	svc.SetWaker(sched)
	h := handler.New(handler.Deps{
		Svc:         svc,
		Send:        bot,
		Parser:      nlparse.Parser{DefaultHour: cfg.DefaultHour, DefaultMinute: cfg.DefaultMinute},
		Clock:       clk,
		Allowed:     cfg.AllowedUsers,
		BotUsername: bot.Username(),
		Log:         log,
	})

	// lead runs while this node holds the poller lease. The lease loop may call
	// it again for the same epoch, so every step is safe to repeat.
	lead := func(lctx context.Context, epoch int64) {
		log.Info("leading: polling telegram", "epoch", epoch)
		if err := bot.DeleteWebhook(lctx); err != nil {
			log.Error("delete webhook", "err", err)
			return
		}
		setCommands(lctx, bot, log)
		if err := bot.Poller().Run(lctx, h.Handle); err != nil {
			log.Error("poller stopped", "err", err)
		}
	}

	// Each loop has its own context so that shutdown can stop them in order.
	leaseCtx, stopLease := context.WithCancel(context.Background())
	defer stopLease()
	schedCtx, stopSched := context.WithCancel(context.Background())
	defer stopSched()
	leaseDone, schedDone := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(leaseDone)
		if err := lease.Run(leaseCtx, st, clk, "poller", cfg.NodeID, cfg.LeaseTTL, lead); err != nil {
			log.Error("release lease", "err", err)
		}
	}()
	go func() {
		defer close(schedDone)
		if err := sched.Run(schedCtx); err != nil {
			log.Error("scheduler", "err", err)
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")
	timeout := time.NewTimer(shutdownTimeout)
	defer timeout.Stop()
	stopLease() // the lease loop stops polling, then releases the lease
	if !waitFor(leaseDone, timeout.C) {
		return errors.New("shutdown timed out stopping the poller")
	}
	stopSched()
	if !waitFor(schedDone, timeout.C) {
		return errors.New("shutdown timed out stopping the scheduler")
	}
	return nil
}

func waitFor(done <-chan struct{}, timeout <-chan time.Time) bool {
	select {
	case <-done:
		return true
	case <-timeout:
		return false
	}
}

// setCommands registers the menu in English (the default) and then in Farsi.
// A failure is only logged: the bot works without a menu.
func setCommands(ctx context.Context, bot *tgbot.Bot, log *slog.Logger) {
	for _, l := range []struct {
		lang string // Telegram language_code; "" is the default
		loc  i18n.Locale
	}{{"", i18n.Locale{Lang: i18n.En}}, {"fa", i18n.Locale{Lang: i18n.Fa}}} {
		cmds := make([]tgbot.Command, 0, len(menu))
		for _, name := range menu {
			cmds = append(cmds, tgbot.Command{Name: name, Description: l.loc.T("cmd.menu." + name)})
		}
		if err := bot.SetCommands(ctx, cmds, l.lang); err != nil {
			log.Warn("set commands", "lang", l.lang, "err", err)
		}
	}
}
