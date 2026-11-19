package objectstore

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Local stores objects below a single root directory. Keys are cleaned
// against a "/" root before joining so "../" can never escape the root.
type Local struct {
	root string
}

var _ Backend = (*Local)(nil)

func NewLocal(root string) *Local {
	if strings.TrimSpace(root) == "" {
		root = "./metadata"
	}
	return &Local{root: root}
}

func (l *Local) resolve(key string) (string, error) {
	if strings.TrimSpace(key) == "" {
		return "", errors.New("objectstore: empty key")
	}
	cleaned := filepath.Clean("/" + key)
	full := filepath.Join(l.root, cleaned)
	absRoot, err := filepath.Abs(l.root)
	if err != nil {
		return "", err
	}
	absFull, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	if absFull != absRoot && !strings.HasPrefix(absFull, absRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("objectstore: invalid key %q (path traversal)", key)
	}
	return full, nil
}

func (l *Local) Name() string { return "local" }

func (l *Local) Put(_ context.Context, key string, data []byte, _ string) error {
	full, err := l.resolve(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	// Write to a temporary file in the same directory and rename, so a crash
	// never leaves a half-written lyric/cover behind.
	tmp, err := os.CreateTemp(filepath.Dir(full), ".obj-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, full)
}

func (l *Local) Get(_ context.Context, key string) ([]byte, string, error) {
	full, err := l.resolve(key)
	if err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(full)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	return data, mimeForName(full), nil
}

func (l *Local) Delete(_ context.Context, key string) error {
	full, err := l.resolve(key)
	if err != nil {
		return err
	}
	if err := os.Remove(full); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (l *Local) Stat(_ context.Context, key string) (int64, int64, error) {
	full, err := l.resolve(key)
	if err != nil {
		return 0, 0, err
	}
	info, err := os.Stat(full)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, 0, ErrNotFound
	}
	if err != nil {
		return 0, 0, err
	}
	return info.Size(), info.ModTime().UnixMilli(), nil
}

func (l *Local) List(_ context.Context, prefix string) ([]string, error) {
	cleaned := strings.Trim(filepath.ToSlash(filepath.Clean("/"+prefix)), "/")
	root, err := filepath.Abs(l.root)
	if err != nil {
		return nil, err
	}
	var keys []string
	err = filepath.WalkDir(root, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(rel)
		if cleaned == "" || strings.HasPrefix(key, cleaned+"/") {
			keys = append(keys, key)
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Strings(keys)
	return keys, nil
}

func (l *Local) TestConnection(ctx context.Context) error {
	root, err := filepath.Abs(l.root)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	const probe = ".objectstore-probe"
	if err := l.Put(ctx, probe, []byte("ok"), "text/plain"); err != nil {
		return err
	}
	return l.Delete(ctx, probe)
}

func mimeForName(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".lrc", ".txt":
		return "text/plain; charset=utf-8"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		return "image/jpeg"
	}
}
