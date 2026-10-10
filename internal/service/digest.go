package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/recur"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
)

var hhmmRE = regexp.MustCompile(`^(\d{1,2}):(\d{2})$`)

// parseHHMM validates user input (Persian digits accepted) and returns the
// canonical "HH:MM".
func parseHHMM(in string) (h, m int, canon string, err error) {
	g := hhmmRE.FindStringSubmatch(i18n.NormalizeDigits(in))
	if g == nil {
		return 0, 0, "", ErrBadTime
	}
	h, _ = strconv.Atoi(g[1])
	m, _ = strconv.Atoi(g[2])
	if h > 23 || m > 59 {
		return 0, 0, "", ErrBadTime
	}
	return h, m, fmt.Sprintf("%02d:%02d", h, m), nil
}

// nextDigestAt returns the first local hhmm strictly after now, in loc. It
// reuses the daily recurrence rule so DST gaps and repeats are handled once.
func nextDigestAt(hhmm string, loc *time.Location, now time.Time) (time.Time, error) {
	h, m, _, err := parseHHMM(hhmm)
	if err != nil {
		return time.Time{}, err
	}
	next := recur.Rule{Freq: recur.Daily, Hour: h, Minute: m}.Next(now, loc)
	if next.IsZero() {
		return time.Time{}, ErrBadTime
	}
	return next.UTC(), nil
}

// SetDigest implements Service.
func (s *Svc) SetDigest(ctx context.Context, a Actor, hhmm string) error {
	if hhmm == "" {
		if err := s.st.SetDigest(ctx, a.ChatID, "", 0, nil); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil // no chat row yet: nothing to turn off
			}
			return err
		}
		return nil
	}
	_, _, canon, err := parseHHMM(hhmm)
	if err != nil {
		return err
	}
	now := s.clk.Now().UTC()
	err = s.st.WithTx(ctx, func(tx store.Store) error {
		u, err := s.ensureUser(ctx, tx, a.UserID, "")
		if err != nil {
			return err
		}
		if err := s.ensureChat(ctx, tx, a, now); err != nil {
			return err
		}
		next, err := nextDigestAt(canon, s.locOrDefault(u.TZ), now)
		if err != nil {
			return err
		}
		return tx.SetDigest(ctx, a.ChatID, canon, a.UserID, &next)
	})
	if err != nil {
		return err
	}
	s.wake()
	return nil
}

func (s *Svc) ensureChat(ctx context.Context, st store.Store, a Actor, now time.Time) error {
	return st.EnsureChat(ctx, model.Chat{ID: a.ChatID, Kind: a.ChatKind, Title: a.ChatTitle, CreatedAt: now})
}

// DigestView implements Service. "Today" is the local calendar day of the
// user who set the digest (or, for a private chat with no digest, the chat's
// owner); overdue items are open todos due before that day started.
func (s *Svc) DigestView(ctx context.Context, chatID int64, now time.Time) (model.DigestView, error) {
	by := int64(0)
	chat, err := s.st.GetChat(ctx, chatID)
	switch {
	case err == nil:
		by = chat.DigestBy
		if by == 0 && chat.Kind == model.ChatPrivate {
			by = chat.ID
		}
	case errors.Is(err, store.ErrNotFound):
	default:
		return model.DigestView{}, err
	}
	v := model.DigestView{ChatID: chatID, Loc: s.cfg.DefaultTZ, Locale: i18n.Default("")}
	if by != 0 {
		u, err := s.st.GetUser(ctx, by)
		switch {
		case err == nil:
			v.Loc = s.locOrDefault(u.TZ)
			v.Locale = i18n.Locale{Lang: u.Lang, Cal: u.Calendar}
		case errors.Is(err, store.ErrNotFound):
		default:
			return model.DigestView{}, err
		}
	}
	todos, err := s.st.ListTodos(ctx, model.TodoQuery{ChatID: chatID, Statuses: []model.TodoStatus{model.TodoOpen}})
	if err != nil {
		return model.DigestView{}, err
	}
	y, m, d := now.In(v.Loc).Date()
	start := time.Date(y, m, d, 0, 0, 0, 0, v.Loc)
	end := time.Date(y, m, d+1, 0, 0, 0, 0, v.Loc)
	for _, t := range todos {
		switch {
		case t.DueAt == nil:
			v.NoDateCount++
		case t.DueAt.Before(start):
			v.Overdue = append(v.Overdue, t)
		case t.DueAt.Before(end):
			v.Today = append(v.Today, t)
		}
	}
	return v, nil
}

// DigestDone implements scheduler.Backend: it stores the chat's next digest
// time (strictly after now, so a digest that was skipped for being late moves
// to the next day) and releases the claim.
func (s *Svc) DigestDone(ctx context.Context, c model.Chat, now time.Time) error {
	if c.DigestTime == "" {
		return nil
	}
	loc := s.cfg.DefaultTZ
	if c.DigestBy != 0 {
		u, err := s.st.GetUser(ctx, c.DigestBy)
		if err == nil {
			loc = s.locOrDefault(u.TZ)
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
	}
	next, err := nextDigestAt(c.DigestTime, loc, now)
	if err != nil {
		return fmt.Errorf("service: digest done for chat %d: %w", c.ID, err)
	}
	ok, err := s.st.AdvanceDigest(ctx, c.ID, c.DigestClaimToken, next)
	if err != nil {
		return err
	}
	if !ok {
		s.log.Debug("service: digest claim lost", "chat", c.ID)
		return nil
	}
	s.wake()
	return nil
}
