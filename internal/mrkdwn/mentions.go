package mrkdwn

import "slices"

// Refs are what a text refers to: the users it mentions with <@…> and the
// channels it references with <#…> without a label (the IDs a renderer
// needs names for), the user groups it mentions with <!subteam^…>, and
// whether it broadcasts with <!here>, <!channel> or <!everyone>. Each
// list is in order, once each; code is not searched.
type Refs struct {
	Users, Channels []string
	Groups          []string
	Broadcast       bool
}

// References returns what s refers to.
func References(s string) Refs {
	var r Refs
	for _, b := range Parse(s, nil) {
		if l, ok := b.(Line); ok {
			r.add(l.Inlines)
		}
	}
	return r
}

func (r *Refs) add(inlines []Inline) {
	for _, in := range inlines {
		switch in := in.(type) {
		case User:
			r.Users = appendNew(r.Users, in.ID)
		case Channel:
			if in.Label == "" {
				r.Channels = appendNew(r.Channels, in.ID)
			}
		case Special:
			r.addSpecial(in)
		case Styled:
			r.add(in.Inlines)
		}
	}
}

func (r *Refs) addSpecial(s Special) {
	switch s.Name() {
	case "subteam":
		r.Groups = appendNew(r.Groups, s.Arg())
	case "here", "channel", "everyone":
		r.Broadcast = true
	}
}

func appendNew(ids []string, id string) []string {
	if slices.Contains(ids, id) {
		return ids
	}
	return append(ids, id)
}
