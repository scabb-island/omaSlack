package mrkdwn

import "slices"

// Refs are the IDs a text refers to that a renderer needs names for:
// users it mentions with <@…>, and channels it references with <#…>
// without a label. Each list is in order, once each; code is not
// searched.
type Refs struct {
	Users, Channels []string
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
		case Styled:
			r.add(in.Inlines)
		}
	}
}

func appendNew(ids []string, id string) []string {
	if slices.Contains(ids, id) {
		return ids
	}
	return append(ids, id)
}
