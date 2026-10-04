package feishu

import "testing"

func TestClearUserProfileCache(t *testing.T) {
	p := &Platform{}
	p.userProfileCache.Store("ou_a", feishuUserProfileEntry{ok: true, name: "A"})
	if n := p.ClearUserProfileCache(); n != 1 {
		t.Fatalf("n=%d", n)
	}
	if _, ok := p.userProfileCache.Load("ou_a"); ok {
		t.Fatal("cache should be empty")
	}
}
