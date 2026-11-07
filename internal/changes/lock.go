package changes

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// lock takes an exclusive flock on the lock file. The kernel releases it
// if the process dies, so a crash never leaves a stale lock.
func (s *Store) lock() (func() error, error) {
	f, err := os.OpenFile(filepath.Join(s.dir, "lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, err
	}
	return func() error {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		return f.Close()
	}, nil
}
