package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/recur"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
)

// loadTZ resolves an IANA name; "" and "Local" are rejected.
func loadTZ(name string) (*time.Location, error) {
	if name == "" || name == "Local" {
		return nil, ErrBadTZ
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, ErrBadTZ
	}
	return loc, nil
}

// locOrDefault resolves a stored zone name, falling back to the default zone.
func (s *Svc) locOrDefault(name string) *time.Location {
	if loc, err := loadTZ(name); err == nil {
		return loc
	}
	return s.cfg.DefaultTZ
}

func prefsOf(u model.User, loc *time.Location) model.Prefs {
	return model.Prefs{TZ: loc, Locale: i18n.Locale{Lang: u.Lang, Cal: u.Calendar}}
}

// ensureUser returns the user, creating it with defaults on first contact.
func (s *Svc) ensureUser(ctx context.Context, st store.Store, userID int64, tgLang string) (model.User, error) {
	u, err := st.GetUser(ctx, userID)
	if err == nil {
		return u, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return model.User{}, err
	}
	loc := i18n.Default(tgLang)
	u = model.User{
		ID:        userID,
		TZ:        s.cfg.DefaultTZ.String(),
		Lang:      loc.Lang,
		Calendar:  loc.Cal,
		CreatedAt: s.clk.Now().UTC(),
	}
	if err := st.UpsertUser(ctx, u); err != nil {
		return model.User{}, err
	}
	return u, nil
}

// Prefs implements Service.
func (s *Svc) Prefs(ctx context.Context, userID int64, tgLang string) (model.Prefs, error) {
	u, err := s.ensureUser(ctx, s.st, userID, tgLang)
	if err != nil {
		return model.Prefs{}, err
	}
	return prefsOf(u, s.locOrDefault(u.TZ)), nil
}

// SetLang implements Service.
func (s *Svc) SetLang(ctx context.Context, userID int64, lang i18n.Lang) error {
	if !lang.Valid() {
		return ErrBadLang
	}
	return s.st.WithTx(ctx, func(tx store.Store) error {
		u, err := s.ensureUser(ctx, tx, userID, "")
		if err != nil {
			return err
		}
		u.Lang = lang
		return tx.UpsertUser(ctx, u)
	})
}

// SetCalendar implements Service.
func (s *Svc) SetCalendar(ctx context.Context, userID int64, cal calendar.Kind) error {
	if !cal.Valid() {
		return ErrBadCalendar
	}
	return s.st.WithTx(ctx, func(tx store.Store) error {
		u, err := s.ensureUser(ctx, tx, userID, "")
		if err != nil {
			return err
		}
		u.Calendar = cal
		return tx.UpsertUser(ctx, u)
	})
}

// SetTZ implements Service. Recurring todos created by the user are
// re-anchored: their next occurrence is recomputed from now in the new zone
// and their pending 'due' reminder replaced. The digests the user set are
// recomputed too. One-off due times are absolute instants and stay put.
func (s *Svc) SetTZ(ctx context.Context, userID int64, tz string) error {
	loc, err := loadTZ(tz)
	if err != nil {
		return err
	}
	now := s.clk.Now().UTC()
	err = s.st.WithTx(ctx, func(tx store.Store) error {
		u, err := s.ensureUser(ctx, tx, userID, "")
		if err != nil {
			return err
		}
		u.TZ = tz
		if err := tx.UpsertUser(ctx, u); err != nil {
			return err
		}
		todos, err := tx.ListRecurringByCreator(ctx, userID)
		if err != nil {
			return err
		}
		for _, t := range todos {
			rule, perr := recur.Parse(t.Recur)
			if perr != nil {
				s.log.Warn("service: skip re-anchor of todo with bad rule", "todo", t.ID, "err", perr)
				continue
			}
			next := rule.Next(now, loc)
			if next.IsZero() {
				continue
			}
			next = next.UTC()
			t.TZ = tz
			t.DueAt = &next
			t.UpdatedAt = now
			if err := tx.UpdateTodo(ctx, t); err != nil {
				return err
			}
			if err := tx.CancelPending(ctx, t.ID, model.ReminderDue); err != nil {
				return err
			}
			r := model.Reminder{TodoID: t.ID, ChatID: t.ChatID, FireAt: next, Kind: model.ReminderDue, State: model.ReminderPending, CreatedAt: now}
			if err := tx.InsertReminder(ctx, &r); err != nil {
				return err
			}
		}
		chats, err := tx.ListChatsByDigestUser(ctx, userID)
		if err != nil {
			return err
		}
		for _, c := range chats {
			next, err := nextDigestAt(c.DigestTime, loc, now)
			if err != nil {
				s.log.Warn("service: skip digest with bad time", "chat", c.ID, "err", err)
				continue
			}
			if err := tx.SetDigest(ctx, c.ID, c.DigestTime, userID, &next); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("service: set tz: %w", err)
	}
	s.wake()
	return nil
}
