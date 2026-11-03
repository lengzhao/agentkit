package feishu

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestProfileResolversHonorCanceledContext(t *testing.T) {
	var calls atomic.Int32
	p, srv := newCardkitMockPlatform(cardkitMockHandler(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":0,"data":{"name":"unexpected","user":{"name":"unexpected"}}}`)
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := p.resolveUserNameContext(ctx, "ou_a"); got != "ou_a" {
		t.Fatalf("name=%q", got)
	}
	if got := p.resolveUserNamesContext(ctx, []string{"ou_b", "ou_b"}); len(got) != 1 || got["ou_b"] != "ou_b" {
		t.Fatalf("names=%v", got)
	}
	if got := p.resolveChatNameContext(ctx, "oc_a"); got != "oc_a" {
		t.Fatalf("chat=%q", got)
	}
	id, name := "ou_c", "Mentioned user"
	if got := p.mentionMetadataContext(ctx, []*larkim.MentionEvent{{Id: &larkim.UserId{OpenId: &id}, Name: &name}}); len(got) == 0 {
		t.Fatal("cancellation should preserve event-provided mention metadata")
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("canceled profile resolvers performed %d lookups", got)
	}
}
func TestProfileResolverContextKeepsCaches(t *testing.T) {
	p := &Platform{}
	p.userProfileCache.Store("ou_cached", feishuUserProfileEntry{ok: true, name: "Cached user"})
	p.chatNameCache.Store("oc_cached", "Cached chat")
	if p.resolveUserNameContext(context.Background(), "ou_cached") != "Cached user" {
		t.Fatal("user cache changed")
	}
	if p.resolveChatNameContext(context.Background(), "oc_cached") != "Cached chat" {
		t.Fatal("chat cache changed")
	}
	if got := p.resolveChatNameContext(context.Background(), "oc_missing"); got != "oc_missing" {
		t.Fatalf("nil-client fallback=%q", got)
	}
}
