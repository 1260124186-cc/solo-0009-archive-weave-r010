package storage

import "sync"

type gate struct{ mu sync.RWMutex }

func (g *gate) read(fn func())  { g.mu.RLock(); defer g.mu.RUnlock(); fn() }
func (g *gate) write(fn func()) { g.mu.Lock(); defer g.mu.Unlock(); fn() }
func lockedRead[T any](g *gate, fn func() T) T {
	var result T
	g.read(func() { result = fn() })
	return result
}
func lockedWrite[T any](g *gate, fn func() T) T {
	var result T
	g.write(func() { result = fn() })
	return result
}
func lockedWrite2[T any](g *gate, fn func() (T, error)) (T, error) {
	var result T
	var err error
	g.write(func() { result, err = fn() })
	return result, err
}
