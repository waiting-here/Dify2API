package handler

import (
	"fmt"
	"sync"
	"testing"
)

func TestAntiAbuseCache_ConcurrentRefreshAndRead(t *testing.T) {
	gw := setupTestGateway(t)
	const iterations = 200
	errCh := make(chan error, 1)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			if _, err := gw.Store.UpsertAntiAbuseConfig("general", i%3, 20+i%5, 0, 0, 1); err != nil {
				select {
				case errCh <- err:
				default:
				}
				return
			}
			if err := gw.loadAntiAbuseCache(); err != nil {
				select {
				case errCh <- err:
				default:
				}
				return
			}
		}
	}()

	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations*4; i++ {
				if cfg := gw.antiAbuseConfig("general"); cfg == nil {
					select {
					case errCh <- fmt.Errorf("general config missing"):
					default:
					}
					return
				}
				if len(gw.antiAbuseConfigList()) == 0 {
					select {
					case errCh <- fmt.Errorf("config list empty"):
					default:
					}
					return
				}
			}
		}()
	}

	wg.Wait()
	select {
	case err := <-errCh:
		t.Fatal(err)
	default:
	}
}
