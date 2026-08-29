package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type req struct {
	ID, Path   string
	Start, End int64
}

func main() {
	server := flag.String("server", os.Getenv("MUSICOLET_AGENT_SERVER"), "server base URL")
	token := flag.String("token", os.Getenv("MUSICOLET_AGENT_TOKEN"), "agent token")
	rootsArg := flag.String("roots", env("MUSICOLET_AGENT_ROOTS", "/storage/emulated/0"), "comma-separated read-only roots")
	allowHTTP := flag.Bool("allow-http", os.Getenv("MUSICOLET_AGENT_ALLOW_HTTP") == "1", "allow insecure HTTP for local development")
	flag.Parse()
	if *server == "" || *token == "" {
		fatal("server and token required")
	}
	u, e := url.Parse(*server)
	if e != nil {
		fatal(e.Error())
	}
	if u.Scheme != "https" && !(*allowHTTP && u.Scheme == "http") {
		fatal("HTTPS required; use -allow-http only for trusted local development")
	}
	roots := []string{}
	for _, r := range strings.Split(*rootsArg, ",") {
		if x, e := filepath.EvalSymlinks(strings.TrimSpace(r)); e == nil {
			roots = append(roots, filepath.Clean(x))
		}
	}
	if len(roots) == 0 {
		fatal("no readable roots")
	}
	client := &http.Client{Timeout: 0}
	backoff := time.Second
	for {
		e = run(client, strings.TrimRight(*server, "/"), *token, roots)
		fmt.Fprintln(os.Stderr, "agent disconnected:", e)
		time.Sleep(backoff)
		if backoff < time.Minute {
			backoff *= 2
		} else {
			backoff = time.Minute
		}
	}
}
func run(client *http.Client, base, token string, roots []string) error {
	r, e := http.NewRequest("GET", base+"/api/agent/connect", nil)
	if e != nil {
		return e
	}
	r.Header.Set("Authorization", "Bearer "+token)
	resp, e := client.Do(r)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("connect status %s", resp.Status)
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 2<<20)
	var event string
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "event: ") {
			event = strings.TrimPrefix(line, "event: ")
			continue
		}
		if event == "read" && strings.HasPrefix(line, "data: ") {
			var q req
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &q) == nil {
				go handle(client, base, token, roots, q)
			}
			event = ""
		}
	}
	return sc.Err()
}
func handle(client *http.Client, base, token string, roots []string, q req) {
	p, e := resolvePath(q.Path)
	if e == nil {
		p, e = securePath(p, roots)
	}
	var data []byte
	if e == nil {
		f, x := os.Open(p)
		if x != nil {
			e = x
		} else {
			defer f.Close()
			if q.Start < 0 {
				q.Start = 0
			}
			if _, x = f.Seek(q.Start, io.SeekStart); x != nil {
				e = x
			} else {
				n := int64(4 << 20)
				if q.End >= q.Start && q.End-q.Start+1 < n {
					n = q.End - q.Start + 1
				}
				data = make([]byte, n)
				var got int
				got, e = f.Read(data)
				if e == io.EOF {
					e = nil
				}
				data = data[:got]
			}
		}
	}
	req, e2 := http.NewRequest("POST", base+"/api/agent/result/"+url.PathEscape(q.ID), bytes.NewReader(data))
	if e2 != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if e != nil {
		req.Header.Set("X-Agent-Error", sanitize(e.Error()))
	}
	resp, e2 := client.Do(req)
	if e2 == nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}
func resolvePath(p string) (string, error) {
	if strings.HasPrefix(p, "file://") {
		u, e := url.Parse(p)
		if e != nil {
			return "", e
		}
		return u.Path, nil
	}
	if strings.HasPrefix(p, "content://com.android.externalstorage.documents/") {
		u, e := url.Parse(p)
		if e != nil {
			return "", e
		}
		idx := strings.Index(u.Path, "/document/")
		if idx < 0 {
			return "", fmt.Errorf("unsupported content URI")
		}
		doc, _ := url.PathUnescape(u.Path[idx+10:])
		parts := strings.SplitN(doc, ":", 2)
		if len(parts) == 2 && parts[0] == "primary" {
			return filepath.Join("/storage/emulated/0", parts[1]), nil
		}
		return "", fmt.Errorf("unsupported external storage volume")
	}
	if strings.HasPrefix(p, "content://") {
		return "", fmt.Errorf("content URI cannot be resolved by the Termux-only agent")
	}
	return p, nil
}
func securePath(p string, roots []string) (string, error) {
	abs, e := filepath.Abs(p)
	if e != nil {
		return "", e
	}
	real, e := filepath.EvalSymlinks(abs)
	if e != nil {
		return "", e
	}
	real = filepath.Clean(real)
	for _, root := range roots {
		rel, e := filepath.Rel(root, real)
		if e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			st, e := os.Stat(real)
			if e != nil {
				return "", e
			}
			if !st.Mode().IsRegular() {
				return "", fmt.Errorf("not a regular file")
			}
			return real, nil
		}
	}
	return "", fmt.Errorf("path outside allowed roots")
}
func sanitize(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func fatal(s string) { fmt.Fprintln(os.Stderr, s); os.Exit(2) }

var _ = strconv.IntSize
