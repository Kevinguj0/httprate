package httprate

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestKeyFuncConcurrency(t *testing.T) {
	limit := 5
	middleware := Limit(
		limit,
		10*time.Second,
		WithKeyFuncs(func(r *http.Request) (string, error) {
			return r.Header.Get("X-Client-ID"), nil
		}),
	)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	var wg sync.WaitGroup
	numClients := 100
	requestsPerClient := 3

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()
			for j := 0; j < requestsPerClient; j++ {
				req := httptest.NewRequest("GET", "/", nil)
				req.Header.Set("X-Client-ID", fmt.Sprintf("client-%d", clientID))
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					t.Errorf("Client %d request %d failed with status %d", clientID, j, rec.Code)
				}
			}
		}(i)
	}

	wg.Wait()
}
