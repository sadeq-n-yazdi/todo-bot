package handler

import "github.com/sadeq-n-yazdi/todo-bot/internal/i18n"

func init() {
	i18n.Register(i18n.En, map[string]string{
		"error.generic": "Something went wrong. Please try again.",
	})
	i18n.Register(i18n.Fa, map[string]string{
		"error.generic": "مشکلی پیش آمد. لطفاً دوباره تلاش کنید.",
	})
}
