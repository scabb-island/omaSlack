// Adapted from github.com/gammons/slk cmd/slk/onboarding_browser_test.go (MIT, Copyright (c) 2026 Grant Ammons).

package console

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// Shapes of "Copy as cURL" as browsers produce them, trimmed to the line
// structure that matters. The values are fake.
const (
	bashCurl = `curl 'https://acme.slack.com/api/conversations.history' \
  -H 'accept: */*' \
  -b 'b=abc123; d=xoxd-AbC%2Fdef; d-s=1700000000' \
  --data-raw 'token=xoxc-1111-2222-3333-abcdef'
`
	oneLineCurl = `curl 'https://acme.slack.com/api/client.counts' -H 'Cookie: d=xoxd-ZZZ' --data-raw 'token=xoxc-9999-8888-aa'
`
	cmdCurl = `curl ^"https://acme.slack.com/api/conversations.history^" ^
  -b ^"b=abc123; d=xoxd-AbC^%^2Fdef^" ^
  --data-raw ^"token=xoxc-1111-2222-3333-abcdef^"
`
	powershellCurl = "$session = New-Object Microsoft.PowerShell.Commands.WebRequestSession\n" +
		"$session.Cookies.Add((New-Object System.Net.Cookie(\"d\", \"xoxd-AbC%2Fdef\", \"/\", \".slack.com\")))\n" +
		"Invoke-WebRequest -UseBasicParsing -Uri \"https://acme.slack.com/api/client.counts\" `\n" +
		"-WebSession $session `\n" +
		"-Headers @{\n" +
		"\"accept\"=\"*/*\"\n" +
		"} `\n" +
		"-Body \"token=xoxc-9999-8888-aa\"\n"
	powershellHeadersCurl = "Invoke-WebRequest -UseBasicParsing -Uri \"https://acme.slack.com/api/client.counts\" `\n" +
		"-Headers @{\n" +
		"\"Cookie\" = \"b=x; d=xoxd-ZZZ\"\n" +
		"} `\n" +
		"-Body \"token=xoxc-9999-8888-aa\"\n"
)

func TestPasteComplete(t *testing.T) {
	for _, tc := range []struct {
		s    string
		want bool
	}{
		{"", false},
		{"\n\n", false},
		{"xoxc-1\n", true},
		{"curl 'x' \\\n", false},
		{"curl 'x' \\  \n", false},
		{"curl 'x' \\\n  -H 'a: b'\n", true},
		{"curl ^\"x^\" ^\n", false},
		{"curl ^\"x^\" ^\n  -H ^\"a: b^\"\n", true},
		{"Invoke-WebRequest -Uri \"x\" `\n", false},
		{"$session = New-Object X\n", false},
		{"$session = New-Object X\nInvoke-WebRequest -Uri \"x\" `\n", false},
		{"$session = New-Object X\nInvoke-WebRequest -Uri \"x\" `\n-Body \"y\"\n", true},
		{"Invoke-WebRequest -Uri \"x\" `\n-Headers @{\n", false},
		{"Invoke-WebRequest -Uri \"x\" `\n-Headers @{\n\"a\"=\"b\"\n", false},
		{"Invoke-WebRequest -Uri \"x\" `\n-Headers @{\n\"a\"=\"b\"\n} `\n", false},
		{"Invoke-WebRequest -Uri \"x\" `\n-Headers @{\n\"a\"=\"b\"\n} `\n-Body \"y\"\n", true},
		{"Invoke-WebRequest -Uri \"x\"\n", true},
	} {
		if got := pasteComplete(tc.s); got != tc.want {
			t.Errorf("pasteComplete(%q) = %v, want %v", tc.s, got, tc.want)
		}
	}
}

// A raw-mode terminal sends \r for Enter; every shape must be read up to
// the line that closes the command, and not past it. A paste read short
// leaves its tail, token and cookie included, for the shell to read.
func TestReadPasteReadsEveryShapeWhole(t *testing.T) {
	for name, paste := range map[string]string{
		"bash": bashCurl, "one line": oneLineCurl, "cmd": cmdCurl,
		"powershell": powershellCurl, "powershell, cookie in the headers": powershellHeadersCurl,
	} {
		in := strings.ReplaceAll(paste, "\n", "\r") + "left over"
		got, err := ReadPaste(strings.NewReader(in))
		if err != nil || got != paste {
			t.Errorf("%s: ReadPaste = (%q, %v), want the whole paste", name, got, err)
		}
	}
}

// A line longer than the 4096-byte read buffer (and than the tty's
// canonical line limit) must come through whole.
func TestReadPasteLongLine(t *testing.T) {
	long := "curl -b 'd=xoxd-" + strings.Repeat("A", 10000) + "'\r"
	got, err := ReadPaste(strings.NewReader(long))
	if want := strings.ReplaceAll(long, "\r", "\n"); err != nil || got != want {
		t.Errorf("ReadPaste returned %d bytes, %v; want %d", len(got), err, len(want))
	}
}

func TestReadPasteControlKeys(t *testing.T) {
	if got, err := ReadPaste(strings.NewReader("xoxc-1\x03")); got != "" || !errors.Is(err, ErrCancelled) {
		t.Errorf("Ctrl-C: ReadPaste = (%q, %v), want nothing and ErrCancelled", got, err)
	}
	got, err := ReadPaste(strings.NewReader("curl 'x' \\\r\x04more"))
	if err != nil || got != "curl 'x' \\\n" {
		t.Errorf("Ctrl-D: ReadPaste = (%q, %v), want what was read so far", got, err)
	}
	got, err = ReadPaste(strings.NewReader("xoxc-1"))
	if err != nil || got != "xoxc-1" {
		t.Errorf("EOF: ReadPaste = (%q, %v), want what was read so far", got, err)
	}
}

type failingReader struct{}

var errBroken = errors.New("broken")

func (failingReader) Read([]byte) (int, error) { return 0, errBroken }

func TestReadPasteReportsReadErrors(t *testing.T) {
	if _, err := ReadPaste(failingReader{}); !errors.Is(err, errBroken) {
		t.Errorf("err = %v, want the read error", err)
	}
}

// fakeTerminal is input arriving on a schedule, for drainInput:
// arrivals[i] bytes land before poll i.
type fakeTerminal struct {
	arrivals []int
	poll     int
	queued   int
	flushes  int
	slept    time.Duration
	failing  bool
}

func (f *fakeTerminal) pending() (int, error) {
	if f.poll < len(f.arrivals) {
		f.queued += f.arrivals[f.poll]
	}
	f.poll++
	if f.failing {
		return f.queued, errBroken
	}
	return f.queued, nil
}
func (f *fakeTerminal) flush() error          { f.flushes++; f.queued = 0; return nil }
func (f *fakeTerminal) sleep(d time.Duration) { f.slept += d }

func TestDrainInput(t *testing.T) {
	t.Run("nothing pending: one quiet window", func(t *testing.T) {
		f := &fakeTerminal{}
		drainInput(f.pending, f.flush, f.sleep)
		if f.slept != 500*time.Millisecond || f.flushes != 1 {
			t.Errorf("slept %v, %d flushes; want 500ms and one final flush", f.slept, f.flushes)
		}
	})
	t.Run("a tail arriving late is still discarded", func(t *testing.T) {
		// 62 bytes 300 ms in, as over a slow SSH link.
		f := &fakeTerminal{arrivals: []int{0, 0, 0, 0, 0, 0, 62}}
		drainInput(f.pending, f.flush, f.sleep)
		if f.queued != 0 || f.flushes != 2 {
			t.Errorf("queued %d after %d flushes, want the tail flushed", f.queued, f.flushes)
		}
		if want := 7*50*time.Millisecond + 500*time.Millisecond; f.slept != want {
			t.Errorf("slept %v, want %v (a full quiet window after the tail)", f.slept, want)
		}
	})
	t.Run("a single byte restarts the quiet window", func(t *testing.T) {
		f := &fakeTerminal{arrivals: []int{1}}
		drainInput(f.pending, f.flush, f.sleep)
		if want := 50*time.Millisecond + 500*time.Millisecond; f.slept != want || f.flushes != 2 {
			t.Errorf("slept %v, %d flushes; want %v and 2", f.slept, f.flushes, want)
		}
	})
	t.Run("a terminal that cannot say counts as quiet", func(t *testing.T) {
		f := &fakeTerminal{arrivals: []int{5}, failing: true}
		drainInput(f.pending, f.flush, f.sleep)
		if f.slept != 500*time.Millisecond || f.flushes != 1 {
			t.Errorf("slept %v, %d flushes; want one quiet window", f.slept, f.flushes)
		}
	})
	t.Run("input that never stops: bounded", func(t *testing.T) {
		arrivals := make([]int, 1000)
		for i := range arrivals {
			arrivals[i] = 1
		}
		f := &fakeTerminal{arrivals: arrivals}
		drainInput(f.pending, f.flush, f.sleep)
		if limit := 5 * time.Second; f.slept != limit {
			t.Errorf("slept %v, want %v", f.slept, limit)
		}
	})
}
