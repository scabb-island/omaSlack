package domain

// BotID identifies a bot integration (B…). Legacy integrations post
// bot_message messages with a bot ID and no user.
type BotID struct{ s string }

// ParseBotID validates s as a bot ID.
func ParseBotID(s string) (BotID, error) {
	return parseID(s, "bot", "B", func(v string) BotID { return BotID{v} })
}

func (id BotID) String() string { return id.s }
func (id BotID) IsZero() bool   { return id.s == "" }

// Author is who wrote a message: exactly one of a user or a bot. The zero
// value means "no author".
type Author struct {
	user UserID
	bot  BotID
}

// UserAuthor is a message written by a person.
func UserAuthor(u UserID) Author { return Author{user: u} }

// BotAuthor is a message posted by a bot integration.
func BotAuthor(b BotID) Author { return Author{bot: b} }

// ParseAuthor reads an author from its string form: a user ID (U…, W…) or
// a bot ID (B…). This is the one place that decides which is which.
func ParseAuthor(s string) (Author, error) {
	if bot, err := ParseBotID(s); err == nil {
		return BotAuthor(bot), nil
	}
	user, err := ParseUserID(s)
	if err != nil {
		return Author{}, err
	}
	return UserAuthor(user), nil
}

// User returns the author's user ID when the author is a person.
func (a Author) User() (UserID, bool) { return a.user, !a.user.IsZero() }

// Bot returns the author's bot ID when the author is a bot.
func (a Author) Bot() (BotID, bool) { return a.bot, !a.bot.IsZero() }

// String is the author's ID, the form ParseAuthor reads back.
func (a Author) String() string {
	if bot, ok := a.Bot(); ok {
		return bot.String()
	}
	return a.user.String()
}

// IsZero reports whether a has no author.
func (a Author) IsZero() bool { return a == Author{} }
