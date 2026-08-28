package common

import "testing"

func TestResolveDomain(t *testing.T) {
	got, err := ResolveDomain("", "https://open.feishu.cn")
	if err != nil || got != "https://open.feishu.cn" {
		t.Fatalf("default: got %q err=%v", got, err)
	}

	got, err = ResolveDomain("https://open.larksuite.com/", "https://open.feishu.cn")
	if err != nil || got != "https://open.larksuite.com" {
		t.Fatalf("custom: got %q err=%v", got, err)
	}

	if _, err := ResolveDomain("://bad", "https://open.feishu.cn"); err == nil {
		t.Fatal("expected invalid domain error")
	}
}

func TestNormalizeSlackAPIURL(t *testing.T) {
	got, err := NormalizeSlackAPIURL("")
	if err != nil || got != "" {
		t.Fatalf("empty: got %q err=%v", got, err)
	}

	got, err = NormalizeSlackAPIURL("https://slack.com/api")
	if err != nil || got != "https://slack.com/api/" {
		t.Fatalf("slash: got %q err=%v", got, err)
	}

	if _, err := NormalizeSlackAPIURL("not-a-url"); err == nil {
		t.Fatal("expected invalid domain error")
	}
}
