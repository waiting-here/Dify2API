package db

import (
	"fmt"
	"sync"
	"testing"
)

func TestSetSettings_ConcurrentBatchesDoNotInterleave(t *testing.T) {
	st, _ := openTemp(t)
	const rounds = 20
	for round := 0; round < rounds; round++ {
		start := make(chan struct{})
		var wg sync.WaitGroup
		for writer := 1; writer <= 2; writer++ {
			writer := writer
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				value := fmt.Sprintf("%d", writer)
				if err := st.SetSettings([]SettingUpdate{
					{Key: "concurrent_a", Value: value},
					{Key: "concurrent_b", Value: value},
				}); err != nil {
					t.Errorf("writer %d: %v", writer, err)
				}
			}()
		}
		close(start)
		wg.Wait()
		a, err := st.GetSetting("concurrent_a")
		if err != nil {
			t.Fatal(err)
		}
		b, err := st.GetSetting("concurrent_b")
		if err != nil {
			t.Fatal(err)
		}
		if a != b {
			t.Fatalf("round %d observed interleaved batch: a=%q b=%q", round, a, b)
		}
	}
}
