package agentclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Ltre/MusicoletWeb/internal/agentproto"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const Version = "musicolet-agent-v1"

type Config struct {
	ServerURL, Token string
	Roots            []string
	AllowHTTP        bool
}

func Run(ctx context.Context, cfg Config, logf func(string, ...any)) error {
	if cfg.ServerURL == "" || cfg.Token == "" {
		return errors.New("MUSICOLET_SERVER_URL and MUSICOLET_AGENT_TOKEN are required")
	}
	roots, err := normalizeRoots(cfg.Roots)
	if err != nil {
		return err
	}
	backoff := time.Second
	for {
		if err = runConnection(ctx, cfg, roots, logf); ctx.Err() != nil {
			return ctx.Err()
		}
		logf("connection ended: %v; reconnecting in %s", err, backoff)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > time.Minute {
			backoff = time.Minute
		}
	}
}

func runConnection(ctx context.Context, cfg Config, roots []string, logf func(string, ...any)) error {
	u, err := url.Parse(cfg.ServerURL)
	if err != nil {
		return err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		if !cfg.AllowHTTP {
			return errors.New("plain HTTP agent transport is disabled; use HTTPS or explicitly set MUSICOLET_AGENT_ALLOW_HTTP=1 for local development")
		}
		u.Scheme = "ws"
	default:
		return fmt.Errorf("unsupported server URL scheme %q", u.Scheme)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/agent/connect"
	header := http.Header{}
	header.Set("Authorization", "Bearer "+cfg.Token)
	ws, _, err := websocket.Dial(ctx, u.String(), &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		return err
	}
	defer ws.CloseNow()
	ws.SetReadLimit(2 << 20)
	if err = wsjson.Write(ctx, ws, agentproto.Message{Type: "hello", Version: Version}); err != nil {
		return err
	}
	logf("connected to %s (read-only roots: %s)", u.String(), strings.Join(roots, ", "))
	pingCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		ticker := time.NewTicker(4 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-pingCtx.Done():
				return
			case <-ticker.C:
				pc, c := context.WithTimeout(pingCtx, 20*time.Second)
				_ = ws.Ping(pc)
				c()
			}
		}
	}()
	for {
		var req agentproto.Message
		if err = wsjson.Read(ctx, ws, &req); err != nil {
			return err
		}
		if req.Type != "read" {
			continue
		}
		response := readOnly(req, roots)
		if err = wsjson.Write(ctx, ws, response); err != nil {
			return err
		}
	}
}

func readOnly(req agentproto.Message, roots []string) agentproto.Message {
	res := agentproto.Message{Type: "read_result", ID: req.ID}
	path, err := resolve(req.Path, roots)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	file, err := os.Open(path)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		res.Error = err.Error()
		return res
	}
	if !info.Mode().IsRegular() {
		res.Error = "requested path is not a regular file"
		return res
	}
	res.Size = info.Size()
	if req.Offset < 0 || req.Offset > res.Size {
		res.Error = "invalid byte offset"
		return res
	}
	length := req.Length
	if length <= 0 || length > 1<<20 {
		length = 1 << 20
	}
	buf := make([]byte, length)
	n, readErr := file.ReadAt(buf, req.Offset)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		res.Error = readErr.Error()
		return res
	}
	res.Data = buf[:n]
	res.EOF = req.Offset+int64(n) >= res.Size
	return res
}

func normalizeRoots(roots []string) ([]string, error) {
	if len(roots) == 0 {
		roots = []string{"/storage/emulated/0/Music"}
	}
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		abs, err := filepath.Abs(strings.TrimSpace(root))
		if err != nil {
			return nil, err
		}
		resolved, err := filepath.EvalSymlinks(abs)
		if err == nil {
			abs = resolved
		}
		out = append(out, filepath.Clean(abs))
	}
	return out, nil
}

func resolve(source string, roots []string) (string, error) {
	candidate := source
	if strings.HasPrefix(source, "content://") {
		u, err := url.Parse(source)
		if err != nil {
			return "", err
		}
		decoded, err := url.PathUnescape(u.Path)
		if err != nil {
			return "", err
		}
		if index := strings.Index(decoded, "primary:"); index >= 0 {
			candidate = filepath.Join("/storage/emulated/0", filepath.FromSlash(strings.TrimPrefix(decoded[index+len("primary:"):], "/")))
		} else {
			return "", errors.New("unsupported Android content URI volume")
		}
	}
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	for _, root := range roots {
		rel, err := filepath.Rel(root, resolved)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("path is outside configured read-only roots (%s/%s)", runtime.GOOS, runtime.GOARCH)
}
