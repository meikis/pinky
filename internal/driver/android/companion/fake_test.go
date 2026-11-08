package companion

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakePhone stands in for adb and the companion app: a secure settings
// table, and a TCP server that speaks the protocol while Pinky's service is
// listed in enabled_accessibility_services. Like adb, it accepts
// connections when the service is off and closes them at once.
type fakePhone struct {
	t  *testing.T
	ln net.Listener

	mu        sync.Mutex
	settings  map[string]string // secure settings; absent means unset
	shell     []string          // every shell command, in order
	forwards  int
	installed []string
	installs  int
	requests  []map[string]any
	skew      time.Duration // phone clock minus host clock
	// rewrite simulates Android persisting the list again in its own form
	// while the service is on.
	rewrite func(string) string
	// dropAfter closes a connection after that many requests on it.
	dropAfter int
	setText   func(req map[string]any) (any, *Error)
	apkSum    string // sha256 of the installed APK
	conns     []net.Conn
	events    chan string
}

func newFakePhone(t *testing.T) *fakePhone {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &fakePhone{t: t, ln: ln, settings: map[string]string{}, events: make(chan string, 16)}
	go p.serve()
	t.Cleanup(func() {
		ln.Close()
		p.mu.Lock()
		for _, c := range p.conns {
			c.Close()
		}
		p.mu.Unlock()
	})
	return p
}

func (p *fakePhone) on() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.ContainsFunc(parseServices(p.settings[settingKey]), New(nil).isOurs)
}

func (p *fakePhone) serve() {
	for {
		c, err := p.ln.Accept()
		if err != nil {
			return
		}
		if !p.on() {
			c.Close()
			continue
		}
		p.mu.Lock()
		p.conns = append(p.conns, c)
		p.mu.Unlock()
		go p.handle(c)
	}
}

func (p *fakePhone) handle(c net.Conn) {
	defer c.Close()
	rd := bufio.NewReader(c)
	n := 0
	for {
		line, err := rd.ReadBytes('\n')
		if err != nil {
			return
		}
		var req map[string]any
		if err := json.Unmarshal(line, &req); err != nil {
			p.t.Errorf("bad request %q: %v", line, err)
			return
		}
		p.mu.Lock()
		p.requests = append(p.requests, req)
		drop := p.dropAfter
		p.mu.Unlock()
		result, perr := p.answer(req)
		resp := map[string]any{"id": req["id"], "ok": perr == nil}
		if perr != nil {
			resp["error"] = perr
		} else {
			resp["result"] = result
		}
		b, _ := json.Marshal(resp)
		if _, err := c.Write(append(b, '\n')); err != nil {
			return
		}
		if req["cmd"] == "events" {
			for ev := range p.events {
				if _, err := c.Write([]byte(ev + "\n")); err != nil {
					return
				}
			}
			return
		}
		n++
		if drop > 0 && n >= drop {
			return
		}
	}
}

func (p *fakePhone) answer(req map[string]any) (any, *Error) {
	switch req["cmd"] {
	case "ping":
		p.mu.Lock()
		skew := p.skew
		p.mu.Unlock()
		return map[string]any{"protocol": 1, "version": "0.1.0", "sdk": 29, "time": time.Now().Add(skew).UnixMilli(), "displays": []int{0}}, nil
	case "tree":
		return json.RawMessage(sampleTree), nil
	case "set_text":
		if p.setText != nil {
			return p.setText(req)
		}
		return map[string]any{"window": 41, "node": 4242, "text": req["text"], "verified": true}, nil
	case "wait_idle":
		return map[string]any{"settled": true, "waited_ms": 300}, nil
	case "config":
		return map[string]any{}, nil
	case "events":
		return map[string]any{"kinds": req["kinds"]}, nil
	}
	return nil, &Error{Code: "unknown_command", Message: fmt.Sprint(req["cmd"])}
}

func (p *fakePhone) lastRequest(cmd string) map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := len(p.requests) - 1; i >= 0; i-- {
		if p.requests[i]["cmd"] == cmd {
			return p.requests[i]
		}
	}
	return nil
}

// Runner implementation.

func (p *fakePhone) Shell(_ context.Context, cmd string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.shell = append(p.shell, cmd)
	f := strings.Fields(cmd)
	switch {
	case len(f) == 4 && f[0] == "settings" && f[1] == "get" && f[2] == "secure":
		v, ok := p.settings[f[3]]
		if !ok {
			return "null\n", nil
		}
		return v + "\n", nil
	case len(f) >= 4 && f[0] == "settings" && f[1] == "put" && f[2] == "secure":
		rest := strings.TrimPrefix(cmd, "settings put secure "+f[3]+" ")
		v, err := unquote(rest)
		if err != nil {
			return "", err
		}
		if p.rewrite != nil {
			v = p.rewrite(v)
		}
		p.settings[f[3]] = v
		return "", nil
	case len(f) == 4 && f[0] == "settings" && f[1] == "delete" && f[2] == "secure":
		delete(p.settings, f[3])
		return "Deleted 1 rows\n", nil
	case len(f) == 3 && f[0] == "pm" && f[1] == "path":
		if slices.Contains(p.installed, f[2]) {
			return "package:/data/app/~~abc/" + f[2] + "-xyz/base.apk\n", nil
		}
		return "", fmt.Errorf("exit status 1")
	case len(f) == 2 && f[0] == "sha256sum":
		return p.apkSum + "  " + strings.Trim(f[1], "'") + "\n", nil
	case len(f) == 3 && f[0] == "dumpsys" && f[1] == "package":
		if slices.Contains(p.installed, f[2]) {
			return "Packages:\n  Package [" + f[2] + "] (1a2b3c):\n    versionCode=1 minSdk=29 targetSdk=36\n    versionName=0.1.0\n", nil
		}
		return "", nil
	}
	return "", fmt.Errorf("fake shell: unexpected command %q", cmd)
}

func (p *fakePhone) Forward(context.Context, string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.forwards++
	return p.ln.Addr().String(), nil
}

func (p *fakePhone) RemoveForward(context.Context, string) error { return nil }

func (p *fakePhone) Install(_ context.Context, apk string) error {
	sum, err := fileSHA256(apk)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !slices.Contains(p.installed, Release.Package) {
		p.installed = append(p.installed, Release.Package)
	}
	p.apkSum = sum
	p.installs++
	return nil
}

// unquote reverses shellQuote.
func unquote(s string) (string, error) {
	if len(s) < 2 || s[0] != '\'' || s[len(s)-1] != '\'' {
		return "", fmt.Errorf("not single-quoted: %q", s)
	}
	return strings.ReplaceAll(s[1:len(s)-1], `'\''`, `'`), nil
}
