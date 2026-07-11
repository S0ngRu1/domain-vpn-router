//go:build !windows

package singleinstance

type Lock struct{}

func Acquire(name string) (*Lock, bool, error) {
	return &Lock{}, false, nil
}

func (l *Lock) Release() {}
