package listeners

import (
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"strconv"

	"github.com/pkg/errors"
)

type File struct {
	Path  string      `yaml:"path"`
	Mode  os.FileMode `yaml:"mode"`
	User  string      `yaml:"user"`
	Group string      `yaml:"group"`
}

func (f File) Write(writeFn func(file io.Writer) error) (bool, error) {
	file, err := os.CreateTemp(filepath.Dir(f.Path), ".consul-publish-")
	if err != nil {
		return false, errors.Wrap(err, "create temp file")
	}

	defer os.RemoveAll(file.Name())

	mode := coalesce(f.Mode, 0o644)
	if err := file.Chmod(mode); err != nil {
		return false, errors.Wrap(err, "chmod temp file")
	}

	uid, gid, err := f.owner()
	if err != nil {
		return false, errors.Wrap(err, "get uid and gid")
	}

	if err := file.Chown(uid, gid); err != nil {
		return false, errors.Wrap(err, "chown temp file")
	}

	if err := writeFn(file); err != nil {
		return false, errors.Wrap(err, "write content")
	}

	if err := file.Close(); err != nil {
		return false, errors.Wrap(err, "close temp file")
	}

	same, err := f.isSame(file.Name())
	if err != nil {
		return false, errors.Wrap(err, "check if same file")
	}

	log := slog.With("path", f.Path)

	if same {
		log.Debug("file unchanged")
		return false, nil
	}

	if err := os.Rename(file.Name(), f.Path); err != nil {
		return false, errors.Wrap(err, "rename temp file")
	}

	log.Info("updated file")
	return true, nil
}

func (f File) isSame(tempPath string) (bool, error) {
	target, err := hashSHA256(f.Path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	case err != nil:
		return false, errors.Wrap(err, "hash target file")
	}

	temp, err := hashSHA256(tempPath)
	if err != nil {
		return false, errors.Wrap(err, "hash temp file")
	}

	return temp == target, nil
}

func (f File) owner() (uid int, gid int, err error) {
	username := coalesce(f.User, "root")
	u, err := user.Lookup(username)
	if err != nil {
		return 0, 0, errors.Wrap(err, "lookup user")
	}

	uid, err = strconv.Atoi(u.Uid)
	if err != nil {
		return 0, 0, errors.Wrap(err, "convert uid")
	}

	groupname := coalesce(f.Group, "root")
	g, err := user.LookupGroup(groupname)
	if err != nil {
		return 0, 0, errors.Wrap(err, "lookup group")
	}

	gid, err = strconv.Atoi(g.Gid)
	if err != nil {
		return 0, 0, errors.Wrap(err, "convert gid")
	}

	return uid, gid, nil
}

func hashSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", errors.Wrap(err, "open file")
	}

	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", errors.Wrap(err, "read file")
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func coalesce[T comparable](values ...T) T {
	var zero T
	for _, value := range values {
		if value != zero {
			return value
		}
	}

	return zero
}
