package domain

// User is one person in a workspace, as user search returns them.
type User struct {
	ID          UserID
	Name        string // the handle (@name)
	RealName    string
	DisplayName string // empty when the user never set one
}

// SearchHit is one message a message search found, with where it lives.
type SearchHit struct {
	Conversation ConversationID
	Message      Message
}
