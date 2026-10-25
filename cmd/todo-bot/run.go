package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"runtime/debug"
	"strings"
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
	"github.com/sadeq-n-yazdi/todo-bot/internal/store/sqlite" // registers the "sqlite" scheme
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg/tgbot"
	"github.com/sadeq-n-yazdi/todo-bot/internal/web"
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

// Backoff for the first connection to Telegram; tests shorten it.
var (
	startBackoffMin = time.Second
	startBackoffMax = 30 * time.Second
)

// maxStartWait caps any single wait before a getMe retry, including a
// Telegram RetryAfter. A longer flood wait would outlast the deploy health
// check; the service retries again after the cap. Tests shorten it.
var maxStartWait = 5 * time.Minute

// openStore opens the database; tests wrap it.
var openStore = store.Open

// openExisting opens a sqlite database that must exist, without migrating it.
// Only backup uses it; store.Open cannot.
var openExisting = func(ctx context.Context, raw string) (store.Store, error) {
	d, err := sqlite.OpenExistingURL(ctx, raw)
	if err != nil {
		return nil, err // not a typed nil inside the interface
	}
	return d, nil
}

// listenWeb opens the listener of the web UI; tests wrap it.
var listenWeb = net.Listen

// tgServerURL points the Telegram client at another server; tests set it.
var tgServerURL string

// menu lists the bot commands shown in Telegram's menu, in order.
var menu = []string{"help", "new", "add", "list", "done", "del", "edit", "today", "remind", "tz", "digest", "lang", "calendar"}

const usage = "usage: todo-bot [run|version|migrate|backup PATH]\n" +
	"  backup opens an existing database without migrating it. It fails if the\n" +
	"  database file does not exist.\n"

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
	ui := "placeholder"
	if web.Embedded() {
		ui = "embedded"
	}
	return fmt.Sprintf("todo-bot %s (commit %s, built %s)\nweb: %s", version, c, d, ui)
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
// With existing set, a sqlite database must already be there and is not
// migrated (sqlite.OpenExistingURL); other schemes open as usual.
func openAdmin(ctx context.Context, env func(string) string, existing bool) (store.Store, error) {
	cfg, err := loadAdminConfig(env)
	if err != nil {
		return nil, err
	}
	open := openStore
	if u, perr := url.Parse(cfg.DBURL); existing && perr == nil && u.Scheme == "sqlite" {
		open = openExisting
	}
	st, err := open(ctx, cfg.DBURL)
	if err != nil {
		return nil, fmt.Errorf("open database %s: %w", redactURL(cfg.DBURL), err)
	}
	return st, nil
}

func migrate(ctx context.Context, env func(string) string, stdout io.Writer) error {
	st, err := openAdmin(ctx, env, false) // opening applies pending migrations
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

// backup writes a consistent copy of the database to dest. It opens the
// database without creating it and without migrating it, so a newer binary
// never changes the live database before it copies it.
func backup(ctx context.Context, env func(string) string, dest string, stdout io.Writer) error {
	st, err := openAdmin(ctx, env, true)
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

	st, err := openStore(ctx, cfg.DBURL)
	if err != nil {
		return fmt.Errorf("open database %s: %w", redactURL(cfg.DBURL), err)
	}
	// The database is closed last, and only after the leader and the scheduler
	// have stopped. If they did not stop in time, Close could hang on a query
	// that is still running, so it is skipped: the process exits right after.
	closeDB := true
	defer func() {
		if !closeDB {
			return
		}
		if err := st.Close(); err != nil {
			log.Error("close database", "err", err)
		}
	}()

	bot, err := connectTelegram(ctx, cfg.Token, log)
	if err != nil {
		return err
	}
	if bot == nil { // interrupted while waiting for Telegram
		return nil
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

	// The web UI starts only when TODO_WEB_ADDR has a value, after the
	// service exists and before the lease loop (docs/WEB-DESIGN.md §9.2).
	webSrv, err := startWeb(ctx, cfg, svc, bot, clk, log)
	if err != nil {
		return err
	}

	// With the web UI on, the observer notes which allowlisted users write in
	// which groups. It wraps the handler and never grants access (§4.3).
	handle := h.Handle
	if webSrv != nil {
		handle = web.Observe(handle, svc, cfg.AllowedUsers, log)
	}
	// The "Open app" buttons under /start and /help exist only with the web
	// UI on. An empty origin turns them off.
	appOrigin := ""
	if webSrv != nil {
		appOrigin = cfg.WebOrigin
	}
	handler.SetWebApp(appOrigin)

	// lead runs while this node holds the poller lease. The lease loop may call
	// it again for the same epoch, so every step is safe to repeat.
	lead := func(lctx context.Context, epoch int64) {
		log.Info("leading: polling telegram", "epoch", epoch)
		if err := bot.DeleteWebhook(lctx); err != nil {
			if lctx.Err() == nil { // a cancelled lead is expected, not an error
				log.Error("delete webhook", "err", err)
			}
			return
		}
		setCommands(lctx, bot, log)
		if webSrv != nil { // with the web off, the menu button does not change
			setChatMenuButton(lctx, bot, cfg.WebOrigin, log)
		}
		if err := bot.Poller().Run(lctx, handle); err != nil && lctx.Err() == nil {
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
	log.Info("shutting down", "budget", shutdownTimeout) // one budget for the whole shutdown
	timeout := time.NewTimer(shutdownTimeout)
	defer timeout.Stop()
	// The web stops at the same time as the lease loop, inside the same
	// budget: it gets no time of its own.
	// webErr is set before webDone closes. Shutdown returns an error when its
	// deadline ends while a request still runs: then the web did not stop,
	// although webDone is closed.
	webDone := make(chan struct{})
	var webErr error
	if webSrv == nil {
		close(webDone)
	} else {
		go func() {
			defer close(webDone)
			wctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(shutdownTimeout))
			defer cancel()
			if webErr = webSrv.Shutdown(wctx); webErr != nil {
				log.Warn("stop web server", "err", webErr)
			}
		}()
	}
	// fail gives up: the database stays open, because a loop may still use it.
	fail := func(what string) error {
		closeDB = false
		if webSrv != nil {
			_ = webSrv.Close()
		}
		return errors.New("shutdown timed out stopping the " + what)
	}
	stopLease() // the lease loop stops polling, then releases the lease
	if !waitFor(leaseDone, timeout.C) {
		return fail("poller")
	}
	stopSched()
	if !waitFor(schedDone, timeout.C) {
		return fail("scheduler")
	}
	if !waitFor(webDone, timeout.C) || webErr != nil {
		return fail("web server")
	}
	return nil
}

// startWeb starts the web UI on cfg.WebAddr, or returns nil if the address
// is empty. It first deletes the sessions of the users who are not in the
// allowlist (an allowlist change needs a restart). The listener opens here,
// so a bind error stops the start.
func startWeb(ctx context.Context, cfg config.Config, svc *service.Svc, bot *tgbot.Bot, clk clock.Clock, log *slog.Logger) (*web.Server, error) {
	if cfg.WebAddr == "" {
		return nil, nil
	}
	n, err := svc.PruneWebSessions(ctx, cfg.AllowedUsers)
	if err != nil {
		return nil, fmt.Errorf("web: prune sessions: %w", err)
	}
	if n > 0 {
		log.Info("web: removed the sessions of users not allowed", "sessions", n)
	}
	srv := web.New(web.Deps{
		Svc:     svc,
		Web:     svc,
		Send:    bot,
		Members: bot,
		Parser:  nlparse.Parser{DefaultHour: cfg.DefaultHour, DefaultMinute: cfg.DefaultMinute},
		Clock:   clk,
		Allowed: cfg.AllowedUsers,
		Token:   cfg.Token,
		BotName: bot.Username(),
		Config:  cfg,
		Assets:  web.Assets(),
		Log:     log,
	})
	l, err := listenWeb("tcp", cfg.WebAddr)
	if err != nil {
		return nil, fmt.Errorf("web: listen on %s: %w", cfg.WebAddr, err)
	}
	log.Info("web: listening", "addr", l.Addr().String(), "ui", map[bool]string{true: "embedded", false: "placeholder"}[web.Embedded()])
	go func() {
		if err := srv.Serve(l); err != nil {
			log.Error("web server", "err", err)
		}
	}()
	return srv, nil
}

// connectTelegram calls getMe, retrying with a capped backoff on transient
// errors, so that a short outage at boot does not fail the service. Any other
// error, such as a rejected token (4xx other than 429), is fatal. It returns
// (nil, nil) if ctx ended while waiting.
func connectTelegram(ctx context.Context, token string, log *slog.Logger) (*tgbot.Bot, error) {
	delay := startBackoffMin
	for {
		b, err := tgbot.New(ctx, token, tgbot.Options{ServerURL: tgServerURL})
		if err == nil {
			return b, nil
		}
		if ctx.Err() != nil {
			return nil, nil
		}
		wait, ok := retryWait(err, delay)
		if !ok {
			return nil, fmt.Errorf("telegram: %w", err)
		}
		wait = min(wait, maxStartWait)
		log.Warn("telegram unavailable, retrying", "in", wait, "err", err)
		select {
		case <-ctx.Done():
			return nil, nil
		case <-time.After(wait):
		}
		delay = min(delay*2, startBackoffMax)
	}
}

// retryWait decides whether a failed getMe is worth retrying and for how
// long to wait. Only network and transport errors (*url.Error implements
// net.Error), 5xx, 429 and response-body decode errors are retried. A proxy
// or gateway that answers 502 with an HTML page gives a decode error. On a
// 429 the wait is at least the RetryAfter that Telegram sent; the caller
// caps it at maxStartWait.
func retryWait(err error, delay time.Duration) (time.Duration, bool) {
	var ae *tg.APIError
	if errors.As(err, &ae) {
		switch {
		case ae.Code == 429:
			return max(delay, ae.RetryAfter), true
		case ae.Code >= 500:
			return delay, true
		}
		return 0, false
	}
	var syn *json.SyntaxError
	var typ *json.UnmarshalTypeError
	var ne net.Error
	if errors.As(err, &syn) || errors.As(err, &typ) || errors.As(err, &ne) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return delay, true
	}
	return 0, false
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

// setChatMenuButton sets the default menu button of private chats to open
// the Mini App at origin + "/tg/". The menu button has no language, so its
// title is English. A failure is only logged: the bot works without it.
func setChatMenuButton(ctx context.Context, bot *tgbot.Bot, origin string, log *slog.Logger) {
	title := i18n.Locale{Lang: i18n.En}.T("todo.app.menu")
	if err := bot.SetMenuButton(ctx, title, strings.TrimRight(origin, "/")+"/tg/"); err != nil && ctx.Err() == nil {
		log.Warn("set menu button", "err", err)
	}
}
