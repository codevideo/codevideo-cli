package types

import "fmt"

type RenderStrategy string

const (
	RenderStrategySequentialSeek RenderStrategy = "sequential-seek"
	RenderStrategyAbInitio       RenderStrategy = "ab-initio"
)

func ResolveRenderStrategy(strategy RenderStrategy) (RenderStrategy, error) {
	if strategy == "" {
		return RenderStrategySequentialSeek, nil
	}
	if strategy != RenderStrategySequentialSeek && strategy != RenderStrategyAbInitio {
		return "", fmt.Errorf("unknown render strategy %q; use sequential-seek or ab-initio", strategy)
	}
	return strategy, nil
}
