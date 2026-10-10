# todo-bot

A self-hosted Telegram todo bot with reminders, for you and your family.

- Todo lists: a personal list in your private chat, and a shared list in a group.
- Reminders:
  - one-off: "tomorrow 18:00", "in 2h"
  - recurring: daily, weekdays, weekly, monthly, every N days
  - snooze buttons
  - a daily digest
- **English and Farsi (فارسی)** interfaces, **Gregorian and Jalali (هجری شمسی)** calendars.
- A single static Go binary with SQLite. It runs as a hardened systemd service in less than 40 MB of RAM.

> 🚧 Under construction. Progress: [docs/PROGRESS.md](docs/PROGRESS.md).

## Documentation
- [Usage](docs/USAGE.md)
- [Deployment](docs/DEPLOYMENT.md)
- [Architecture](docs/ARCHITECTURE.md)
- [Changelog](CHANGELOG.md)

## License
[MIT](LICENSE)
