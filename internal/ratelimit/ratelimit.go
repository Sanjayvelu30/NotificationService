package ratelimit

import (
	"fmt"
	"sync"
	"time"
)

type RateLimitConfig struct {
	MaxTokens int
	Duration  time.Duration
}

type RatelimitStrategy interface {
	Allow(time.Time) (bool, error)
}

type FixedWindow struct {
	Config          RateLimitConfig
	TokensRemaining int
	WindowStart     time.Time
	Mu              sync.Mutex
}

func (f *FixedWindow) Allow(now time.Time) (bool, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()

	// If the current time has crossed the window duration, reset the window
	if now.Sub(f.WindowStart) >= f.Config.Duration {
		f.WindowStart = now
		f.TokensRemaining = f.Config.MaxTokens
	}

	if f.TokensRemaining <= 0 {
		return false, nil
	}

	f.TokensRemaining--
	return true, nil
}

type RateLimitRepo interface {
	GetOrCreate(key string) RatelimitStrategy
}

type InMemoryRateLimitRepo struct {
	Mu      sync.RWMutex
	Windows map[string]RatelimitStrategy
	Config  RateLimitConfig
}

func NewInMemoryRateLimitRepo(config RateLimitConfig) *InMemoryRateLimitRepo {
	return &InMemoryRateLimitRepo{
		Windows: make(map[string]RatelimitStrategy),
		Config:  config,
	}
}

func (i *InMemoryRateLimitRepo) GetOrCreate(key string) RatelimitStrategy {
	i.Mu.RLock()
	strategy, isFound := i.Windows[key]
	if isFound {
		i.Mu.RUnlock()
		return strategy
	}
	i.Mu.RUnlock()

	i.Mu.Lock()
	defer i.Mu.Unlock()

	// Double-check lock pattern
	strategy, isFound = i.Windows[key]
	if isFound {
		return strategy
	}

	window := &FixedWindow{
		Config:          i.Config,
		TokensRemaining: i.Config.MaxTokens,
		WindowStart:     time.Now(),
	}

	i.Windows[key] = window
	return window
}

type RateLimitSystem struct {
	Repo RateLimitRepo
}

func NewRateLimitSystem(repo RateLimitRepo) *RateLimitSystem {
	return &RateLimitSystem{Repo: repo}
}

func (r *RateLimitSystem) Allow(key string) error {
	strategy := r.Repo.GetOrCreate(key)
	allowed, err := strategy.Allow(time.Now())
	if err != nil {
		return err
	}
	if !allowed {
		return fmt.Errorf("rate limit exceeded: max requests reached for this period")
	}
	return nil
}
