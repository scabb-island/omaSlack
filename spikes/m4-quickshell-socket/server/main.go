// Command spike-server is the daemon side of spike M4.1: can Quickshell's
// Socket + SplitParser carry the omaslack NDJSON protocol reliably?
//
// It speaks a tiny JSON-RPC-over-NDJSON dialect on a unix socket:
//
//	hello            -> {"protocol":1}
//	burst{count}     -> count "event" notifications as fast as possible, with
//	                    unicode, escaped newlines and some very large lines,
//	                    then a result with the count and a checksum
//	ping             -> {"pong":true}   (round-trip latency)
//	trickle{ms,n}    -> n events, one every ms, while the client keeps pinging
//	drop             -> closes the connection (client must reconnect)
//
// Throwaway code: it exists to answer one question, not to become the daemon.
package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

type request struct {
	ID     int             `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type conn struct {
	mu sync.Mutex
	w  *bufio.Writer
	c  net.Conn
}

func (c *conn) send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := c.w.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

func (c *conn) flush() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.w.Flush()
}

func (c *conn) reply(id int, result any) error {
	if err := c.send(map[string]any{"jsonrpc": "2.0", "id": id, "result": result}); err != nil {
		return err
	}
	return c.flush()
}

// eventText returns a message body that exercises the parser: plain text,
// accents and emoji, embedded newlines (escaped as \n by JSON), quotes, and
// every 2000th event a 256 KiB line.
func eventText(seq int) string {
	switch {
	case seq%2000 == 1999:
		return strings.Repeat("long line ✓ é ", 256*1024/16)
	case seq%7 == 0:
		return fmt.Sprintf("message %d with\nseveral\nlines and \"quotes\"", seq)
	case seq%5 == 0:
		return fmt.Sprintf("accents áéíóú ñ and emoji 🚀🔥 #%d", seq)
	default:
		return fmt.Sprintf("message %d", seq)
	}
}

func handle(nc net.Conn) {
	defer nc.Close()
	c := &conn{w: bufio.NewWriterSize(nc, 64*1024), c: nc}
	sc := bufio.NewScanner(nc)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var req request
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			log.Printf("bad request %q: %v", sc.Text(), err)
			continue
		}
		var err error
		switch req.Method {
		case "hello":
			err = c.reply(req.ID, map[string]any{"protocol": 1})
		case "ping":
			err = c.reply(req.ID, map[string]any{"pong": true})
		case "burst":
			err = burst(c, req)
		case "trickle":
			go func() {
				if err := trickle(c, req); err != nil {
					log.Printf("trickle: %v", err)
				}
			}()
		case "drop":
			log.Printf("dropping connection on request")
			return
		default:
			err = c.send(map[string]any{"jsonrpc": "2.0", "id": req.ID,
				"error": map[string]any{"code": -32601, "message": "Method not found"}})
			if err == nil {
				err = c.flush()
			}
		}
		if err != nil {
			log.Printf("%s: %v", req.Method, err)
			return
		}
	}
	if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) {
		log.Printf("read: %v", err)
	}
}

func burst(c *conn, req request) error {
	var p struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return err
	}
	h := sha256.New()
	bytes := 0
	start := time.Now()
	for seq := range p.Count {
		text := eventText(seq)
		h.Write([]byte(text))
		bytes += len(text)
		if err := c.send(map[string]any{"jsonrpc": "2.0", "method": "event",
			"params": map[string]any{"seq": seq, "text": text}}); err != nil {
			return err
		}
	}
	if err := c.flush(); err != nil {
		return err
	}
	log.Printf("burst: %d events, %d text bytes in %s", p.Count, bytes, time.Since(start))
	return c.reply(req.ID, map[string]any{"sent": p.Count, "textBytes": bytes,
		"sha256": hex.EncodeToString(h.Sum(nil))})
}

func trickle(c *conn, req request) error {
	var p struct {
		Every int `json:"everyMs"`
		N     int `json:"n"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return err
	}
	t := time.NewTicker(time.Duration(p.Every) * time.Millisecond)
	defer t.Stop()
	for i := range p.N {
		<-t.C
		if err := c.send(map[string]any{"jsonrpc": "2.0", "method": "tick",
			"params": map[string]any{"seq": i}}); err != nil {
			return err
		}
		if err := c.flush(); err != nil {
			return err
		}
	}
	return c.reply(req.ID, map[string]any{"ticks": p.N})
}

func main() {
	path := flag.String("socket", os.Getenv("XDG_RUNTIME_DIR")+"/omaslack-spike.sock", "unix socket path")
	delay := flag.Duration("delay", 0, "wait this long before listening (tests a client that starts first)")
	flag.Parse()

	time.Sleep(*delay)
	_ = os.Remove(*path)
	ln, err := net.Listen("unix", *path)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.Chmod(*path, 0o600); err != nil {
		log.Fatal(err)
	}
	log.Printf("listening on %s", *path)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		_ = ln.Close()
		_ = os.Remove(*path)
		os.Exit(0)
	}()

	for {
		nc, err := ln.Accept()
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("client connected")
		go handle(nc)
	}
}
