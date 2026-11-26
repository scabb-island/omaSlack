package domain

// FileID identifies a file shared in Slack (F…). It is also a directory
// name in the download cache, which its well-formedness makes safe.
type FileID struct{ s string }

// ParseFileID validates s as a file ID.
func ParseFileID(s string) (FileID, error) {
	return parseID(s, "file", "F", func(v string) FileID { return FileID{v} })
}

func (id FileID) String() string { return id.s }
func (id FileID) IsZero() bool   { return id.s == "" }

// File is one file attached to a message: what clients show, and what the
// daemon needs to download it later.
type File struct {
	ID       FileID
	Name     string // the file name as uploaded; outside data, never a path
	Title    string // empty when the uploader set none
	MIMEType string
	Size     int64 // in bytes, as Slack reports it

	// DownloadURL is where the bytes are (Slack's url_private_download),
	// empty when Slack offers no download (an external file). It needs the
	// provider's credentials, so it stays inside the daemon: it is never
	// sent to clients.
	DownloadURL string
}
