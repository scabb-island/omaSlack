package app

// media is the Model's state for files and inline images: view state and
// caches, never Slack state (the daemon keeps that).
type media struct {
	open    Opener     // opens downloaded files (o)
	chooser *chooser   // the file chooser, while open
	cache   imageCache // inline images (images.go)
}

func newMedia(o Options) media {
	m := media{open: o.Opener, cache: newImageCache(o.Images)}
	if m.open == nil {
		m.open = xdgOpen
	}
	return m
}
