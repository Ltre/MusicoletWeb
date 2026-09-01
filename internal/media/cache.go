package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/Ltre/MusicoletWeb/internal/agenthub"
)

type Cache struct {
	dir   string
	hub   *agenthub.Hub
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func NewCache(dir string, hub *agenthub.Hub) (*Cache, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Cache{dir: dir, hub: hub, locks: map[string]*sync.Mutex{}}, nil
}

func (c *Cache) Path(source string) string {
	sum := sha256.Sum256([]byte(source))
	return filepath.Join(c.dir, hex.EncodeToString(sum[:])+".media")
}

func (c *Cache) Ensure(ctx context.Context, source string) (string, error) {
	target := c.Path(source)
	if info, err := os.Stat(target); err == nil && info.Mode().IsRegular() {
		return target, nil
	}
	lock := c.lock(source)
	lock.Lock()
	defer lock.Unlock()
	if _, err := os.Stat(target); err == nil {
		return target, nil
	}
	tmp, err := os.CreateTemp(c.dir, "download-*.partial")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		tmp.Close()
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	var offset int64
	for {
		msg, err := c.hub.Read(ctx, source, offset, 1<<20)
		if err != nil {
			return "", err
		}
		if len(msg.Data) == 0 && !msg.EOF {
			return "", errors.New("agent returned an empty non-final chunk")
		}
		if _, err = tmp.Write(msg.Data); err != nil {
			return "", err
		}
		offset += int64(len(msg.Data))
		if msg.EOF {
			break
		}
		if msg.Size > 0 && offset > msg.Size {
			return "", fmt.Errorf("agent sent more bytes than declared size %d", msg.Size)
		}
	}
	if err = tmp.Sync(); err != nil {
		return "", err
	}
	if err = tmp.Close(); err != nil {
		return "", err
	}
	if err = os.Rename(tmpName, target); err != nil {
		return "", err
	}
	ok = true
	return target, nil
}

func (c *Cache) Clear(source string) error {
	if source == "" {
		entries, err := os.ReadDir(c.dir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.Type().IsRegular() {
				if err = os.Remove(filepath.Join(c.dir, entry.Name())); err != nil {
					return err
				}
			}
		}
		return nil
	}
	err := os.Remove(c.Path(source))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
func (c *Cache) lock(key string) *sync.Mutex {
	c.mu.Lock()
	defer c.mu.Unlock()
	lock := c.locks[key]
	if lock == nil {
		lock = &sync.Mutex{}
		c.locks[key] = lock
	}
	return lock
}
