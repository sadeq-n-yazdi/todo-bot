package handler

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

// Settings commands. Each replies itself for expected errors (bad input)
// and returns an error only for unexpected failures.

// cmdRemind: "/remind <n> <when>" adds a reminder to item n;
// "/remind <text> <when>" creates an item with a due time.
func (h *Handler) cmdRemind(ctx context.Context, m *tg.Message, r req, args string) error {
	lc := r.Prefs.Locale
	args = strings.TrimSpace(args)
	if args == "" {
		return h.say(ctx, m, lc.T("settings.remind.use"))
	}
	now := h.Clock.Now().In(tzOf(r))

	// "<n> <when>": only if the rest really is a time phrase, so that
	// "/remind 2 pizzas tomorrow" stays an item called "2 pizzas".
	if first, rest, ok := strings.Cut(i18n.NormalizeDigits(args), " "); ok {
		if num, err := strconv.Atoi(first); err == nil && num > 0 {
			if at, rule, err := h.Parser.ParseWhen(rest, now, lc.Cal); err == nil {
				return h.remindItem(ctx, m, r, num, at, rule != nil)
			}
		}
	}

	res, err := h.Parser.Parse(args, now, lc.Cal)
	if err != nil {
		return err
	}
	if res.Due == nil || strings.TrimSpace(res.Text) == "" {
		return h.say(ctx, m, lc.T("settings.remind.nowhen")+"\n\n"+lc.T("settings.remind.use"))
	}
	if res.Rule != nil {
		return h.say(ctx, m, lc.T("settings.remind.recur"))
	}
	td, err := h.Svc.AddTodo(ctx, r.Actor, res)
	if err != nil {
		return err
	}
	return h.say(ctx, m, lc.T("settings.remind.added", lc.Num(td.Num), echo(td.Text),
		ui.FormatWhen(*td.DueAt, lc, tzOf(r), now)))
}

func (h *Handler) remindItem(ctx context.Context, m *tg.Message, r req, num int, at time.Time, recurring bool) error {
	lc := r.Prefs.Locale
	if recurring {
		return h.say(ctx, m, lc.T("settings.remind.recur"))
	}
	td, err := h.Svc.ByNum(ctx, r.Actor.ChatID, num)
	if isGone(err) {
		return h.say(ctx, m, lc.T("settings.remind.nosuch", lc.Num(num)))
	} else if err != nil {
		return err
	}
	rem, err := h.Svc.Remind(ctx, r.Actor, td.ID, at)
	switch {
	case errors.Is(err, service.ErrInPast):
		return h.say(ctx, m, lc.T("settings.remind.past"))
	case isGone(err):
		return h.say(ctx, m, lc.T("settings.remind.nosuch", lc.Num(num)))
	case err != nil:
		return err
	}
	when := ui.FormatWhen(rem.FireAt, lc, tzOf(r), h.Clock.Now())
	return h.say(ctx, m, lc.T("settings.remind.set", when)+"\n"+lc.Num(td.Num)+". "+echo(td.Text))
}

func (h *Handler) cmdTZ(ctx context.Context, m *tg.Message, r req, args string) error {
	lc := r.Prefs.Locale
	name := strings.TrimSpace(args)
	if name == "" {
		return h.say(ctx, m, lc.T("settings.tz.show", ui.Escape(tzOf(r).String())))
	}
	// "Local" would resolve to the server's zone; only IANA names are valid.
	if loc, err := time.LoadLocation(name); err != nil || name == "Local" {
		return h.say(ctx, m, lc.T("settings.tz.bad", echo(clip(name, 64))))
	} else {
		name = loc.String()
	}
	err := h.Svc.SetTZ(ctx, r.Actor.UserID, name)
	if errors.Is(err, service.ErrBadTZ) {
		return h.say(ctx, m, lc.T("settings.tz.bad", echo(clip(name, 64))))
	} else if err != nil {
		return err
	}
	return h.say(ctx, m, lc.T("settings.tz.set", ui.Escape(name)))
}

func (h *Handler) cmdDigest(ctx context.Context, m *tg.Message, r req, args string) error {
	lc := r.Prefs.Locale
	arg := strings.ToLower(strings.TrimSpace(i18n.NormalizeDigits(args)))
	switch arg {
	case "":
		return h.say(ctx, m, lc.T("settings.digest.use"))
	case "off", "خاموش":
		if err := h.Svc.SetDigest(ctx, r.Actor, ""); err != nil {
			return err
		}
		return h.say(ctx, m, lc.T("settings.digest.off"))
	}
	t, err := time.Parse("15:04", arg)
	if err != nil {
		return h.say(ctx, m, lc.T("settings.digest.bad"))
	}
	hhmm := t.Format("15:04")
	err = h.Svc.SetDigest(ctx, r.Actor, hhmm)
	if errors.Is(err, service.ErrBadTime) {
		return h.say(ctx, m, lc.T("settings.digest.bad"))
	} else if err != nil {
		return err
	}
	return h.say(ctx, m, lc.T("settings.digest.set", lc.Digits(hhmm), ui.Escape(tzOf(r).String())))
}

var langAliases = map[string]i18n.Lang{
	"en": i18n.En, "english": i18n.En, "انگلیسی": i18n.En,
	"fa": i18n.Fa, "farsi": i18n.Fa, "persian": i18n.Fa, "فارسی": i18n.Fa,
}

func (h *Handler) cmdLang(ctx context.Context, m *tg.Message, r req, args string) error {
	lc := r.Prefs.Locale
	arg := strings.ToLower(strings.TrimSpace(args))
	if arg == "" {
		row := []tg.Button{}
		for _, l := range []i18n.Lang{i18n.En, i18n.Fa} {
			row = append(row, tg.Button{Text: langName(lc, l), Data: settingData(settingLang, string(l))})
		}
		return h.reply(ctx, m, tg.OutMessage{
			Text:     lc.T("settings.lang.show", langName(lc, lc.Lang)),
			Keyboard: [][]tg.Button{row},
		})
	}
	lang, ok := langAliases[arg]
	if !ok {
		return h.say(ctx, m, lc.T("settings.lang.bad"))
	}
	err := h.Svc.SetLang(ctx, r.Actor.UserID, lang)
	if errors.Is(err, service.ErrBadLang) {
		return h.say(ctx, m, lc.T("settings.lang.bad"))
	} else if err != nil {
		return err
	}
	lc.Lang = lang // confirm in the new language
	return h.say(ctx, m, lc.T("settings.lang.set", langName(lc, lang)))
}

var calAliases = map[string]calendar.Kind{
	"gregorian": calendar.Gregorian, "میلادی": calendar.Gregorian,
	"jalali": calendar.Jalali, "jalaali": calendar.Jalali, "shamsi": calendar.Jalali,
	"شمسی": calendar.Jalali, "جلالی": calendar.Jalali,
}

func (h *Handler) cmdCalendar(ctx context.Context, m *tg.Message, r req, args string) error {
	lc := r.Prefs.Locale
	arg := strings.ToLower(strings.TrimSpace(args))
	if arg == "" {
		row := []tg.Button{}
		for _, k := range []calendar.Kind{calendar.Gregorian, calendar.Jalali} {
			row = append(row, tg.Button{Text: calName(lc, k), Data: settingData(settingCal, string(k))})
		}
		cur := lc.Cal
		if !cur.Valid() {
			cur = calendar.Gregorian
		}
		return h.reply(ctx, m, tg.OutMessage{
			Text:     lc.T("settings.cal.show", calName(lc, cur)),
			Keyboard: [][]tg.Button{row},
		})
	}
	cal, ok := calAliases[arg]
	if !ok {
		return h.say(ctx, m, lc.T("settings.cal.bad"))
	}
	err := h.Svc.SetCalendar(ctx, r.Actor.UserID, cal)
	if errors.Is(err, service.ErrBadCalendar) {
		return h.say(ctx, m, lc.T("settings.cal.bad"))
	} else if err != nil {
		return err
	}
	lc.Cal = cal
	return h.say(ctx, m, lc.T("settings.cal.set", calName(lc, cal)))
}

// clip shortens s to at most n runes.
func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}
