package app

import (
	"sort"
	"strings"
	"sync"
)

// lockTask returns an unlock function for one operation target. Locks are
// created lazily so test-created UseCases and future targets need no plumbing.
func (u *UseCases) lockTask(key string) func() {
	if u == nil || strings.TrimSpace(key) == "" {
		return func() {}
	}
	u.taskMu.Lock()
	if u.taskLocks == nil {
		u.taskLocks = make(map[string]*sync.Mutex)
	}
	lock := u.taskLocks[key]
	if lock == nil {
		lock = &sync.Mutex{}
		u.taskLocks[key] = lock
	}
	u.taskMu.Unlock()
	lock.Lock()
	return lock.Unlock
}

// lockTasks acquires a stable sorted set so two multi-Agent requests cannot
// deadlock while waiting on overlapping targets.
func (u *UseCases) lockTasks(prefix string, targets []string) func() {
	keys := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		if target = strings.TrimSpace(target); target != "" {
			keys[prefix+target] = struct{}{}
		}
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	unlockers := make([]func(), 0, len(ordered))
	for _, key := range ordered {
		unlockers = append(unlockers, u.lockTask(key))
	}
	return func() {
		for index := len(unlockers) - 1; index >= 0; index-- {
			unlockers[index]()
		}
	}
}
