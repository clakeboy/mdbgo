package mdbgo

import (
	"context"
	"errors"
	"runtime"
	"sync"
)

const (
	maxDefaultConcurrentQueries    = 8
	maxConfiguredConcurrentQueries = 64
)

type queryHandlePool struct {
	path string
	sem  chan struct{}

	mu     sync.Mutex
	idle   []*DB
	open   int
	closed bool
	active sync.WaitGroup
}

func defaultMaxConcurrentQueries() int {
	size := runtime.GOMAXPROCS(0)
	if size < 2 {
		size = 2
	}
	if size > maxDefaultConcurrentQueries {
		size = maxDefaultConcurrentQueries
	}
	return size
}

func newQueryHandlePool(path string, size int) *queryHandlePool {
	return &queryHandlePool{
		path: path,
		sem:  make(chan struct{}, size),
	}
}

func (p *queryHandlePool) acquire(ctx context.Context) (*DB, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case p.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		<-p.sem
		return nil, errors.New("db is closed")
	}
	p.active.Add(1)
	if count := len(p.idle); count > 0 {
		session := p.idle[count-1]
		p.idle = p.idle[:count-1]
		p.mu.Unlock()
		return session, nil
	}
	p.open++
	p.mu.Unlock()

	session, err := openDBHandle(p.path)
	if err != nil {
		p.mu.Lock()
		p.open--
		p.mu.Unlock()
		p.active.Done()
		<-p.sem
		return nil, err
	}

	p.mu.Lock()
	if p.closed {
		p.open--
		p.mu.Unlock()
		session.closeQueryHandle()
		p.active.Done()
		<-p.sem
		return nil, errors.New("db is closed")
	}
	p.mu.Unlock()
	return session, nil
}

func (p *queryHandlePool) release(session *DB) {
	if p == nil || session == nil {
		return
	}
	p.mu.Lock()
	closeSession := p.closed
	if closeSession {
		p.open--
	} else {
		p.idle = append(p.idle, session)
	}
	p.mu.Unlock()
	if closeSession {
		session.closeQueryHandle()
	}
	<-p.sem
	p.active.Done()
}

func (p *queryHandlePool) close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	idle := p.idle
	p.idle = nil
	p.open -= len(idle)
	p.mu.Unlock()
	for _, session := range idle {
		session.closeQueryHandle()
	}
	p.active.Wait()
}

func (db *DB) acquireQuerySession(ctx context.Context) (*DB, func(), error) {
	if db == nil {
		return nil, nil, errors.New("db is closed")
	}
	db.stateMu.Lock()
	if db.closed || db.ptr == nil || db.queryPool == nil {
		db.stateMu.Unlock()
		return nil, nil, errors.New("db is closed")
	}
	pool := db.queryPool
	db.stateMu.Unlock()

	session, err := pool.acquire(ctx)
	if err != nil {
		return nil, nil, err
	}
	return session, func() {
		pool.release(session)
	}, nil
}
