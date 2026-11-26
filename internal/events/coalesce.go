package events

// coalescer decides when unread.summary is sent: at once for a change that
// finds no window open (the leading emission), then at most once per
// window. A window opens with every emission and lasts Options.Interval;
// the caller starts that timer whenever change or tick returns true and
// calls tick when it fires.
type coalescer struct {
	open    bool // a window is running
	pending bool // something changed since the last emission
}

// change records that the summary may have changed and reports whether to
// send it now.
func (c *coalescer) change() bool {
	if c.open {
		c.pending = true
		return false
	}
	c.open = true
	return true
}

// tick ends the window and reports whether to send the summary now, which
// opens the next one.
func (c *coalescer) tick() bool {
	if !c.pending {
		c.open = false
		return false
	}
	c.pending = false
	return true
}
