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
				_, _ = GetModelRatio("gpt-4")
				_, _ = GetCompletionRatio("gpt-4")
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

func TestUnpricedModelIsRefused(t *testing.T) {
	t.Cleanup(func() {
		_ = UpdateModelRatioByJSONString(ModelRatio2JSONString())
		_ = UpdateCompletionRatioByJSONString(CompletionRatio2JSONString())
	})
	if err := UpdateModelRatioByJSONString(`{}`); err != nil {
		t.Fatalf("reset model ratio: %v", err)
	}
	if err := UpdateCompletionRatioByJSONString(`{}`); err != nil {
		t.Fatalf("reset completion ratio: %v", err)
	}
	if _, err := GetModelRatio("never-priced-model"); err == nil {
		t.Fatal("unpriced model billed instead of refused")
	}
	if _, err := GetCompletionRatio("never-priced-model"); err == nil {
		t.Fatal("model without an output price billed instead of refused")
	}
	if _, ok := LookupModelRatio("never-priced-model"); ok {
		t.Fatal("price listing advertised an unpriced model")
	}
}

func TestContractPricesServeFirstPartyIntegrations(t *testing.T) {
	jevInput, err := GetModelRatio("jev-latest")
	if err != nil {
		t.Fatalf("jev input price: %v", err)
	}
	if jevInput != 0.021 {
		t.Fatalf("jev input ratio = %v, want 0.021", jevInput)
	}
	jevCompletion, err := GetCompletionRatio("jev-latest")
	if err != nil || jevCompletion != 0 {
		t.Fatalf("jev completion ratio = %v, err %v; want 0", jevCompletion, err)
	}
	// The DeepL contract price covers every language code, not a fixed list.
	for _, name := range []string{"deepl-zh", "deepl-en", "deepl-de", "deepl-ko"} {
		input, err := GetModelRatio(name)
		if err != nil || input != 12.5 {
			t.Fatalf("%s input ratio = %v, err %v; want 12.5", name, input, err)
		}
		if completion, err := GetCompletionRatio(name); err != nil || completion != 0 {
			t.Fatalf("%s completion ratio = %v, err %v; want 0", name, completion, err)
		}
	}
}

func TestMergeFetchedModelPricesFillsOnlyMissing(t *testing.T) {
	t.Cleanup(func() {
		_ = UpdateModelRatioByJSONString(ModelRatio2JSONString())
		_ = UpdateCompletionRatioByJSONString(CompletionRatio2JSONString())
	})
	if err := UpdateModelRatioByJSONString(`{"manual-model": 3}`); err != nil {
		t.Fatalf("seed model ratio: %v", err)
	}
	if err := UpdateCompletionRatioByJSONString(`{"manual-model": 2}`); err != nil {
		t.Fatalf("seed completion ratio: %v", err)
	}
	added := MergeFetchedModelPrices(map[string]FetchedModelPrice{
		// already priced by the operator: a sync must not touch it
		"manual-model": {Input: 99, Completion: 99},
		// covered by the contract seed: a sync must not duplicate it
		"jev-latest": {Input: 99, Completion: 99},
		// free model: ratio zero is expressible, so it is priced
		"vendor/free-model": {Input: 0, Completion: 0},
		// free input with paid output cannot be expressed in the charge
		// formula, which multiplies the whole bill by the input ratio
		"vendor/backwards-model": {Input: 0, Completion: 4},
		"vendor/paid-model":      {Input: 1, Completion: 4},
	})
	if added != 2 {
		t.Fatalf("added = %d, want 2 (paid-model and free-model)", added)
	}
	if ratio, _ := LookupModelRatio("manual-model"); ratio != 3 {
		t.Fatalf("manual price overwritten: %v", ratio)
	}
	if ratio, _ := LookupModelRatio("vendor/paid-model"); ratio != 1 {
		t.Fatalf("fetched price not merged: %v", ratio)
	}
	if ratio, _ := LookupCompletionRatio("vendor/paid-model"); ratio != 4 {
		t.Fatalf("fetched completion not merged: %v", ratio)
	}
	if ratio, ok := LookupModelRatio("vendor/backwards-model"); ok {
		t.Fatalf("unexpressible price merged: %v", ratio)
	}
}
