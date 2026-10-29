package types

import "testing"

func TestResolveRenderStrategy(t *testing.T) {
	for _, value := range []RenderStrategy{"", RenderStrategySequentialSeek, RenderStrategyAbInitio} {
		got, err := ResolveRenderStrategy(value)
		if err != nil || got == "" {
			t.Fatalf("resolve %q: %q %v", value, got, err)
		}
	}
	if _, err := ResolveRenderStrategy("unknown"); err == nil {
		t.Fatal("accepted unknown strategy")
	}
}
