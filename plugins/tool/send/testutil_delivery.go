package send

import (
	"testing"

	capsdelivery "github.com/lengzhao/agentkit/cap/delivery"
	rtdelivery "github.com/lengzhao/agentkit/runtime/delivery"
)

func mustAssistant(t *testing.T) capsdelivery.Assistant {
	t.Helper()
	a, err := rtdelivery.NewAssistant(struct{}{}, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	return a
}
