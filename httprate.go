package httprate

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	keyCache   = make(map[*http.Request]string)
	keyCacheMu sync.RWMutex
)

type Config struct {
	KeyFn          func(r *http.Request) (string, error)
	KeyFuncs       []func(r *http.Request) (string, error)
	LimitCounter   LimitCounter
	RequestLimit   int
	WindowLength   time.Duration
	ResponseHandler http.HandlerFunc
}

type Option func(cfg *Config) error

func Limit(requestLimit int, windowLength time.Duration, options ...Option) func(next http.Handler) http.Handler {
	cfg := &Config{
		RequestLimit: requestLimit,
		WindowLength: windowLength,
	}

	for _, opt := range options {
		if err := opt(cfg); err != nil {
			panic(fmt.Sprintf("httprate: option error: %v", err))
		}
	}

	if cfg.KeyFn == nil {
		if len(cfg.KeyFuncs) > 0 {
			cfg.KeyFn = func(r *http.Request) (string, error) {
				keyCacheMu.RLock()
				if val, ok := keyCache[r]; ok {
					keyCacheMu.RUnlock()
					return val, nil
				}
				keyCacheMu.RUnlock()

				var keys []string
				for _, fn := range cfg.KeyFuncs {
					key, err := fn(r)
					if err != nil {
						return "", err
					}
					keys = append(keys, key)
				}
				computedKey := strings.Join(keys, ":")

				keyCacheMu.Lock()
				keyCache[r] = computedKey
				keyCacheMu.Unlock()

				return computedKey, nil
			}
		} else {
			cfg.KeyFn = func(r *http.Request) (string, error) {
				return KeyFunc(LimitByIP)(r)
			}
		}
	}

	if cfg.LimitCounter == nil {
		cfg.LimitCounter = NewLocalCounter(requestLimit, windowLength)
	}

	if cfg.ResponseHandler == nil {
		cfg.ResponseHandler = func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				keyCacheMu.Lock()
				delete(keyCache, r)
				keyCacheMu.Unlock()
			}()

			key, err := cfg.KeyFn(r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			ctx := r.Context()
			ctx = context.WithValue(ctx, LimitKey, key)
			r = r.WithContext(ctx)

			count, err := cfg.LimitCounter.Increment(key, 1)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			if count > cfg.RequestLimit {
				cfg.ResponseHandler(w, r)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func WithKeyFuncs(fns ...func(r *http.Request) (string, error)) Option {
	return func(cfg *Config) error {
		cfg.KeyFuncs = fns
		return nil
	}
}

type contextKey string

const LimitKey contextKey = "limitKey"

type KeyFunc func(r *http.Request) (string, error)

func LimitByIP(r *http.Request) (string, error) {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return "", err
	}
	return ip, nil
}
