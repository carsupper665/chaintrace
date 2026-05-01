package auth

import (
	"chaintrace/utils"
	"sync"
	"time"
)

type RevokedTokenRegistry struct {
	Tokens map[string]*discardedTokenStore
	Mutex  sync.RWMutex
}
type discardedTokenStore struct {
	RevokedUser string
	RemoveAt    time.Time
	ExpireAt    time.Time
}

func NewRevokedTokenRegistry() *RevokedTokenRegistry {
	return &RevokedTokenRegistry{
		Tokens: make(map[string]*discardedTokenStore),
	}
}

func (tr *RevokedTokenRegistry) Add(token, user string, expireAt time.Time) {
	tr.Mutex.Lock()
	defer tr.Mutex.Unlock()
	tr.Tokens[token] = &discardedTokenStore{
		RevokedUser: user,
		RemoveAt:    time.Now(),
		ExpireAt:    expireAt,
	}
}

func (tr *RevokedTokenRegistry) IsRevoked(token string) bool {
	tr.Mutex.RLock()
	defer tr.Mutex.RUnlock()
	if _, exists := tr.Tokens[token]; exists {
		return true
	}
	return false
}

func (tr *RevokedTokenRegistry) ClearEvent() error {
	tr.Mutex.Lock()
	defer tr.Mutex.Unlock()
	var logger = utils.SysLog
	newTokens := make(map[string]*discardedTokenStore)
	for k, v := range tr.Tokens {
		if time.Now().After(v.ExpireAt) {
			logger.Infof("%s's Token expired at %v, Removed.", v.ExpireAt, v.RevokedUser)
			continue
		}
		newTokens[k] = v
	}

	tr.Tokens = newTokens
	return nil
}
