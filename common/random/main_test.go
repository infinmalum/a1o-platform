package random

import "testing"

func TestRandomStringsAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		value := GetRandomString(32)
		if len(value) != 32 {
			t.Fatalf("length = %d, want 32", len(value))
		}
		if seen[value] {
			t.Fatalf("第 %d 次出现重复值", i)
		}
		seen[value] = true
	}
}

func TestGenerateKeyShape(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		key := GenerateKey()
		if len(key) != 48 {
			t.Fatalf("key length = %d, want 48", len(key))
		}
		for _, c := range key {
			alnum := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
			if !alnum {
				t.Fatalf("key 含意外字符 %q", c)
			}
		}
		if seen[key] {
			t.Fatalf("第 %d 次出现重复 key", i)
		}
		seen[key] = true
	}
}
