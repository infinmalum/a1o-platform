package ratio

import (
	"fmt"
	"sync"
	"testing"
)

// TestRatioConcurrentReadWrite drives every reader and writer of the ratio
// maps at once. Run with -race: before the locks covered the JSON helpers and
// the completion ratio, this crashed with a concurrent map read/write fatal.
func TestRatioConcurrentReadWrite(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = GetModelRatio("gpt-4", 1)
				_ = GetCompletionRatio("gpt-4", 1)
				_ = GetGroupRatio("default")
				_ = ModelRatio2JSONString()
				_ = CompletionRatio2JSONString()
				_ = GroupRatio2JSONString()
				_ = ModelRatioNames()
				_ = GroupRatioNames()
			}
		}()
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if err := UpdateModelRatioByJSONString(fmt.Sprintf(`{"gpt-4": %d}`, 15+i)); err != nil {
					t.Errorf("update model ratio: %v", err)
					return
				}
				if err := UpdateCompletionRatioByJSONString(fmt.Sprintf(`{"gpt-4": %d}`, 2+i)); err != nil {
					t.Errorf("update completion ratio: %v", err)
					return
				}
				if err := UpdateGroupRatioByJSONString(fmt.Sprintf(`{"default": %d}`, 1+i)); err != nil {
					t.Errorf("update group ratio: %v", err)
					return
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestGroupRatioNamesSorted(t *testing.T) {
	if err := UpdateGroupRatioByJSONString(`{"beta": 2, "default": 1, "alpha": 1}`); err != nil {
		t.Fatalf("update group ratio: %v", err)
	}
	t.Cleanup(func() {
		_ = UpdateGroupRatioByJSONString(GroupRatio2JSONString())
	})
	names := GroupRatioNames()
	want := []string{"alpha", "beta", "default"}
	if len(names) != len(want) {
		t.Fatalf("names = %v", names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("names = %v, want %v", names, want)
		}
	}
}
