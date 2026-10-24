package tgbot

import (
	"context"
	"fmt"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

// ChatMember calls getChatMember. Where Telegram answered with an error, the
// error is a *tg.APIError with the code and the description (the bot token is
// removed). Network and context errors keep their own type.
func (b *Bot) ChatMember(ctx context.Context, chatID, userID int64) (tg.ChatMember, error) {
	m, err := b.b.GetChatMember(ctx, &bot.GetChatMemberParams{ChatID: chatID, UserID: userID})
	if err != nil {
		return tg.ChatMember{}, b.mapErr(err)
	}
	switch m.Type {
	case models.ChatMemberTypeOwner:
		return tg.ChatMember{Status: tg.MemberCreator}, nil
	case models.ChatMemberTypeAdministrator:
		return tg.ChatMember{Status: tg.MemberAdministrator}, nil
	case models.ChatMemberTypeMember:
		return tg.ChatMember{Status: tg.MemberMember}, nil
	case models.ChatMemberTypeRestricted:
		out := tg.ChatMember{Status: tg.MemberRestricted}
		if m.Restricted != nil {
			out.IsMember = m.Restricted.IsMember
		}
		return out, nil
	case models.ChatMemberTypeLeft:
		return tg.ChatMember{Status: tg.MemberLeft}, nil
	case models.ChatMemberTypeBanned:
		return tg.ChatMember{Status: tg.MemberKicked}, nil
	}
	return tg.ChatMember{}, fmt.Errorf("tgbot: getChatMember: unknown status %q", m.Type)
}

var _ tg.MemberChecker = (*Bot)(nil)
