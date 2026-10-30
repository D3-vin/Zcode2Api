package captcha

import (
	"context"
	"log"
)

// FallbackSolver tries A, then B (manager: claim_solver primary, the other
// as fallback). Empty/error from A is the only trigger — a minted param is final.
type FallbackSolver struct {
	a, b         Solver
	aName, bName string
}

func NewFallbackSolver(primary string, d3, node Solver) Solver {
	if primary == "node" {
		return &FallbackSolver{a: node, b: d3, aName: "node", bName: "d3"}
	}
	return &FallbackSolver{a: d3, b: node, aName: "d3", bName: "node"}
}

func (f *FallbackSolver) Solve(ctx context.Context) (string, string, error) {
	param, region, err := f.a.Solve(ctx)
	if err == nil && param != "" {
		return param, region, nil
	}
	log.Printf("[captcha] %s empty — %s fallback (%v)", f.aName, f.bName, err)
	return f.b.Solve(ctx)
}

func (f *FallbackSolver) Close() {
	if f.a != nil {
		f.a.Close()
	}
	if f.b != nil {
		f.b.Close()
	}
}

var _ Solver = (*FallbackSolver)(nil)
