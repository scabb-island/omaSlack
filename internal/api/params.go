package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/rpc"
)

// params reads one call's params into domain values. Each accessor names
// the member it reads; the first problem is kept and reported by err as
// -32602 Invalid params, so a method reads every member it needs and
// checks once, before acting.
//
// Parsing is lenient where that keeps clients forward compatible: unknown
// members are ignored, and a member set to null counts as absent. It is
// strict about the members a method reads: each must have the right JSON
// type and parse as its domain type.
type params struct {
	dir     Directory
	members map[string]json.RawMessage
	problem error
}

func parseParams(dir Directory, raw json.RawMessage) *params {
	p := &params{dir: dir, members: map[string]json.RawMessage{}}
	if len(raw) > 0 && json.Unmarshal(raw, &p.members) != nil {
		p.problem = invalid("params must be an object")
	}
	return p
}

func invalid(format string, args ...any) *rpc.Error {
	return &rpc.Error{Code: rpc.CodeInvalidParams, Message: fmt.Sprintf(format, args...)}
}

// err is the first problem found, nil when every member read was valid.
func (p *params) err() error { return p.problem }

func (p *params) fail(name, format string, args ...any) {
	if p.problem == nil {
		p.problem = invalid(name+": "+format, args...)
	}
}

// decode reads member name into v and reports whether it was present.
func (p *params) decode(name, kind string, v any) bool {
	raw, ok := p.members[name]
	if !ok || bytes.Equal(raw, []byte("null")) {
		return false
	}
	if json.Unmarshal(raw, v) != nil {
		p.fail(name, "must be %s", kind)
	}
	return true
}

// optString is member name, or "" when absent.
func (p *params) optString(name string) string {
	var s string
	p.decode(name, "a string", &s)
	return s
}

// text is a required string member; empty is allowed only with allowEmpty.
func (p *params) text(name string, allowEmpty bool) string {
	var s string
	switch {
	case !p.decode(name, "a string", &s):
		p.fail(name, "missing")
	case s == "" && !allowEmpty:
		p.fail(name, "must not be empty")
	}
	return s
}

// parsed reads a required string member through parse.
func parsed[T any](p *params, name string, parse func(string) (T, error)) T {
	var s string
	if !p.decode(name, "a string", &s) {
		p.fail(name, "missing")
	}
	return parseWith(p, name, s, parse)
}

// optParsed reads an optional string member through parse; absent is the
// zero value.
func optParsed[T any](p *params, name string, parse func(string) (T, error)) T {
	var s string
	if !p.decode(name, "a string", &s) {
		var zero T
		return zero
	}
	return parseWith(p, name, s, parse)
}

func parseWith[T any](p *params, name, s string, parse func(string) (T, error)) T {
	v, err := parse(s)
	if err != nil && p.problem == nil {
		p.fail(name, "%v", err)
	}
	return v
}

// workspace is the signed-in workspace member "team" names. A valid ID
// that is not signed in is "unknown workspace".
func (p *params) workspace() Workspace {
	id := parsed(p, "team", domain.ParseTeamID)
	if p.problem != nil {
		return Workspace{}
	}
	for _, w := range p.dir.Workspaces() {
		if w.Team == id {
			return w
		}
	}
	p.problem = invalid("unknown workspace")
	return Workspace{}
}

func (p *params) conv() domain.ConversationID {
	return parsed(p, "conv", domain.ParseConversationID)
}

func (p *params) ts(name string) domain.TS { return parsed(p, name, domain.ParseTS) }

func (p *params) optTS(name string) domain.TS { return optParsed(p, name, domain.ParseTS) }

// paths is a required, non-empty array of absolute paths.
func (p *params) paths() []string {
	var paths []string
	switch {
	case !p.decode("paths", "an array of strings", &paths):
		p.fail("paths", "missing")
	case len(paths) == 0:
		p.fail("paths", "must not be empty")
	}
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			p.fail("paths", "%q is not an absolute path", path)
		}
	}
	return paths
}

// limit is member "limit": def when absent, at least 1, and capped at
// most (a larger value is lowered, not refused).
func (p *params) limit(def, most int) int {
	n := def
	if p.decode("limit", "an integer", &n) && n < 1 {
		p.fail("limit", "must be at least 1")
	}
	return min(n, most)
}

// positive is a required integer member of at least 1.
func (p *params) positive(name string) int {
	var n int
	switch {
	case !p.decode(name, "an integer", &n):
		p.fail(name, "missing")
	case n < 1:
		p.fail(name, "must be at least 1")
	}
	return n
}

// choice is an optional string member that must be one of options;
// absent is def.
func (p *params) choice(name, def string, options ...string) string {
	s := p.optString(name)
	if s == "" {
		s = def
	}
	if !slices.Contains(options, s) {
		p.fail(name, "must be one of %s", strings.Join(options, ", "))
	}
	return s
}

// required is a string member that must be one of options.
func (p *params) required(name string, options ...string) string {
	if _, ok := p.members[name]; !ok {
		p.fail(name, "missing")
	}
	return p.choice(name, "", options...)
}

// emoji is a required reaction name; surrounding colons (":tada:") are
// dropped, as people type them that way.
func (p *params) emoji() string {
	name := strings.Trim(p.text("emoji", false), ":")
	if name == "" && p.problem == nil {
		p.fail("emoji", "must not be empty")
	}
	return name
}
