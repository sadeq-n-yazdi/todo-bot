package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
)

type actorKey struct{}

// withActor is the actor middleware of docs/WEB-DESIGN.md §5.5. It serves
// every route under /api/chats/{chat}/ and runs after the session check. It
// is the only code that reads {chat}; handlers read the actor with
// actorFrom.
//
//  1. A {chat} that is not an int64 gives 404 not_found.
//  2. The user's own ID gives the private actor, with the stored title.
//  3. A negative ID goes to groupActor (§4.4).
//  4. Any other ID, for example the private chat of another user, gives 404.
func (s *Server) withActor(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chat, err := strconv.ParseInt(r.PathValue("chat"), 10, 64)
		if err != nil {
			s.fail(w, r, errNotFound)
			return
		}
		uid := sessionFrom(r).UserID
		var a service.Actor
		switch {
		case chat == uid:
			a, err = s.privateActor(r.Context(), uid)
		case chat < 0:
			a, err = s.groupActor(r.Context(), uid, chat)
		default:
			err = errNotFound
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), actorKey{}, a)))
	})
}

// privateActor is the actor of the user's own private chat. The title is
// that of the chats row, or "" if there is no row.
func (s *Server) privateActor(ctx context.Context, uid int64) (service.Actor, error) {
	a := service.Actor{UserID: uid, ChatID: uid, ChatKind: model.ChatPrivate}
	c, err := s.d.Web.Chat(ctx, uid)
	switch {
	case err == nil:
		a.ChatTitle = c.Title
	case !errors.Is(err, service.ErrNotFound):
		return service.Actor{}, err
	}
	return a, nil
}

// actorFrom returns the actor that withActor put in ctx. It is the zero
// Actor outside the routes under {chat}.
func actorFrom(ctx context.Context) service.Actor {
	a, _ := ctx.Value(actorKey{}).(service.Actor)
	return a
}
