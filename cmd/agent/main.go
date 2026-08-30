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
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var version = "dev"

const maxChunk = int64(4 << 20)

var mediaURI = regexp.MustCompile(`^content://media/(?:external|internal)(?:_[^/]+)?/audio/media/[0-9]+$`)

type req struct {
	ID, Path   string
	Start, End int64
}

type readResult struct {
	Data       []byte
	Start, End int64
	Size       int64
}

func main() {
	server := flag.String("server", os.Getenv("MUSICOLET_AGENT_SERVER"), "server base URL")
	token := flag.String("token", os.Getenv("MUSICOLET_AGENT_TOKEN"), "agent token")
	rootsArg := flag.String("roots", env("MUSICOLET_AGENT_ROOTS", "/storage/emulated/0"), "comma-separated read-only roots")
	allowHTTP := flag.Bool("allow-http", os.Getenv("MUSICOLET_AGENT_ALLOW_HTTP") == "1", "allow insecure HTTP for local development")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("musicolet-agent", version)
		return
	}
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
		connectedAt := time.Now()
		e = run(client, strings.TrimRight(*server, "/"), *token, roots)
		fmt.Fprintln(os.Stderr, "agent disconnected:", e)
		if time.Since(connectedAt) >= 30*time.Second {
			backoff = time.Second
		}
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
	sem := make(chan struct{}, 4)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "event: ") {
			event = strings.TrimPrefix(line, "event: ")
			continue
		}
		if event == "read" && strings.HasPrefix(line, "data: ") {
			var q req
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &q) == nil {
				sem <- struct{}{}
				go func() { defer func() { <-sem }(); handle(client, base, token, roots, q) }()
			}
			event = ""
		}
	}
	return sc.Err()
}

func handle(client *http.Client, base, token string, roots []string, q req) {
	rr, e := readRange(q.Path, q.Start, q.End, roots)
	body := rr.Data
	req, e2 := http.NewRequest("POST", base+"/api/agent/result/"+url.PathEscape(q.ID), bytes.NewReader(body))
	if e2 != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if e != nil {
		req.Header.Set("X-Agent-Error", sanitize(e.Error()))
	} else {
		req.Header.Set("X-Agent-Start", strconv.FormatInt(rr.Start, 10))
		req.Header.Set("X-Agent-End", strconv.FormatInt(rr.End, 10))
		req.Header.Set("X-Agent-Size", strconv.FormatInt(rr.Size, 10))
	}
	resp, e2 := client.Do(req)
	if e2 == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

func readRange(source string, start, end int64, roots []string) (readResult, error) {
	if start < 0 {
		start = 0
	}
	if end < start || end-start+1 > maxChunk {
		end = start + maxChunk - 1
	}
	if mediaURI.MatchString(source) {
		return readContentMedia(source, start, end)
	}
	p, e := resolvePath(source)
	if e != nil {
		return readResult{}, e
	}
	p, e = securePath(p, roots)
	if e != nil {
		return readResult{}, e
	}
	f, e := os.Open(p)
	if e != nil {
		return readResult{}, e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return readResult{}, e
	}
	sz := st.Size()
	if start >= sz {
		return readResult{}, fmt.Errorf("range not satisfiable")
	}
	if end >= sz {
		end = sz - 1
	}
	if _, e = f.Seek(start, io.SeekStart); e != nil {
		return readResult{}, e
	}
	b := make([]byte, end-start+1)
	n, e := io.ReadFull(f, b)
	if e == io.EOF || e == io.ErrUnexpectedEOF {
		e = nil
	}
	if e != nil {
		return readResult{}, e
	}
	b = b[:n]
	actualEnd := start + int64(n) - 1
	return readResult{Data: b, Start: start, End: actualEnd, Size: sz}, nil
}

func readContentMedia(uri string, start, end int64) (readResult, error) {
	if !mediaURI.MatchString(uri) {
		return readResult{}, fmt.Errorf("unsupported content URI")
	}
	sz, err := contentMediaSize(uri)
	if err != nil {
		return readResult{}, err
	}
	if start >= sz {
		return readResult{}, fmt.Errorf("range not satisfiable")
	}
	if end >= sz {
		end = sz - 1
	}
	cmd := exec.Command("/system/bin/content", "read", "--uri", uri)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return readResult{}, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err = cmd.Start(); err != nil {
		return readResult{}, err
	}
	if start > 0 {
		if _, err = io.CopyN(io.Discard, stdout, start); err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return readResult{}, err
		}
	}
	want := end - start + 1
	b := make([]byte, want)
	n, readErr := io.ReadFull(stdout, b)
	if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
		readErr = nil
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	if readErr != nil {
		return readResult{}, readErr
	}
	b = b[:n]
	return readResult{Data: b, Start: start, End: start + int64(n) - 1, Size: sz}, nil
}

func contentMediaSize(uri string) (int64, error) {
	cmd := exec.Command("/system/bin/content", "query", "--uri", uri, "--projection", "_size")
	o, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("content query: %v: %s", err, sanitize(string(o)))
	}
	re := regexp.MustCompile(`(?:^|[ ,])_size=([0-9]+)`)
	m := re.FindStringSubmatch(string(o))
	if len(m) != 2 {
		return 0, fmt.Errorf("content provider did not return _size")
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid content size")
	}
	return n, nil
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
		return "", fmt.Errorf("unsupported content URI")
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
