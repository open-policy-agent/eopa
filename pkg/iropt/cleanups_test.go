package iropt_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/open-policy-agent/opa/v1/ir"
	"github.com/open-policy-agent/eopa/pkg/iropt"
)

func TestEmptyLoopReplacement(t *testing.T) {
	tests := []struct {
		note              string
		initial           []*ir.Block
		expected          []*ir.Block
		err               error
		expectNotModified bool
	}{
		{
			note:              "empty loop replacement on empty block",
			initial:           []*ir.Block{},
			expected:          []*ir.Block{},
			expectNotModified: true,
		},
		{
			note: "empty loop replacement on simple empty loop",
			initial: []*ir.Block{
				{
					Stmts: []ir.Stmt{
						&ir.ScanStmt{
							Source: 3, Key: 4, Value: 5, Block: &ir.Block{
								Stmts: []ir.Stmt{},
							},
						},
					},
				},
			},
			expected: []*ir.Block{
				{
					Stmts: []ir.Stmt{
						&ir.IsDefinedStmt{Source: 3},
						&ir.MakeNumberIntStmt{Value: int64(0), Target: 4},
						&ir.LenStmt{Source: ir.Operand{Value: ir.Local(3)}, Target: 5},
						&ir.NotEqualStmt{A: ir.Operand{Value: ir.Local(4)}, B: ir.Operand{Value: ir.Local(5)}},
					},
				},
			},
		},
		{
			note: "empty loop replacement on simple non-empty loop",
			initial: []*ir.Block{
				{
					Stmts: []ir.Stmt{
						&ir.ScanStmt{
							Source: 3, Key: 4, Value: 5, Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.NopStmt{},
								},
							},
						},
					},
				},
			},
			expected: []*ir.Block{
				{
					Stmts: []ir.Stmt{
						&ir.ScanStmt{
							Source: 3, Key: 4, Value: 5, Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.NopStmt{},
								},
							},
						},
					},
				},
			},
			expectNotModified: true,
		},
		{
			note: "empty loop replacement on a nested empty loop (block)",
			initial: []*ir.Block{
				{
					Stmts: []ir.Stmt{
						&ir.BlockStmt{
							Blocks: []*ir.Block{
								{
									Stmts: []ir.Stmt{
										&ir.NopStmt{},
										&ir.ScanStmt{
											Source: 6, Key: 7, Value: 8, Block: &ir.Block{
												Stmts: []ir.Stmt{},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expected: []*ir.Block{
				{
					Stmts: []ir.Stmt{
						&ir.BlockStmt{
							Blocks: []*ir.Block{
								{
									Stmts: []ir.Stmt{
										&ir.NopStmt{},
										&ir.IsDefinedStmt{Source: 6},
										&ir.MakeNumberIntStmt{Value: int64(0), Target: 7},
										&ir.LenStmt{Source: ir.Operand{Value: ir.Local(6)}, Target: 8},
										&ir.NotEqualStmt{A: ir.Operand{Value: ir.Local(7)}, B: ir.Operand{Value: ir.Local(8)}},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			note: "empty loop replacement on a nested empty loop (not)",
			initial: []*ir.Block{
				{
					Stmts: []ir.Stmt{
						&ir.NotStmt{
							Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.NopStmt{},
									&ir.ScanStmt{
										Source: 6, Key: 7, Value: 8, Block: &ir.Block{
											Stmts: []ir.Stmt{},
										},
									},
								},
							},
						},
					},
				},
			},
			expected: []*ir.Block{
				{
					Stmts: []ir.Stmt{
						&ir.NotStmt{
							Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.NopStmt{},
									&ir.IsDefinedStmt{Source: 6},
									&ir.MakeNumberIntStmt{Value: int64(0), Target: 7},
									&ir.LenStmt{Source: ir.Operand{Value: ir.Local(6)}, Target: 8},
									&ir.NotEqualStmt{A: ir.Operand{Value: ir.Local(7)}, B: ir.Operand{Value: ir.Local(8)}},
								},
							},
						},
					},
				},
			},
		},
		{
			note: "empty loop replacement on a nested empty loop (with)",
			initial: []*ir.Block{
				{
					Stmts: []ir.Stmt{
						&ir.WithStmt{
							Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.NopStmt{},
									&ir.ScanStmt{
										Source: 6, Key: 7, Value: 8, Block: &ir.Block{
											Stmts: []ir.Stmt{},
										},
									},
								},
							},
						},
					},
				},
			},
			expected: []*ir.Block{
				{
					Stmts: []ir.Stmt{
						&ir.WithStmt{
							Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.NopStmt{},
									&ir.IsDefinedStmt{Source: 6},
									&ir.MakeNumberIntStmt{Value: int64(0), Target: 7},
									&ir.LenStmt{Source: ir.Operand{Value: ir.Local(6)}, Target: 8},
									&ir.NotEqualStmt{A: ir.Operand{Value: ir.Local(7)}, B: ir.Operand{Value: ir.Local(8)}},
								},
							},
						},
					},
				},
			},
		},
		{
			note: "empty loop replacement on a nested empty loop (scan)",
			initial: []*ir.Block{
				{
					Stmts: []ir.Stmt{
						&ir.ScanStmt{
							Source: 9, Key: 4, Value: 5, Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.NopStmt{},
									&ir.ScanStmt{
										Source: 6, Key: 7, Value: 8, Block: &ir.Block{
											Stmts: []ir.Stmt{},
										},
									},
								},
							},
						},
					},
				},
			},
			expected: []*ir.Block{
				{
					Stmts: []ir.Stmt{
						&ir.ScanStmt{
							Source: 9, Key: 4, Value: 5, Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.NopStmt{},
									&ir.IsDefinedStmt{Source: 6},
									&ir.MakeNumberIntStmt{Value: int64(0), Target: 7},
									&ir.LenStmt{Source: ir.Operand{Value: ir.Local(6)}, Target: 8},
									&ir.NotEqualStmt{A: ir.Operand{Value: ir.Local(7)}, B: ir.Operand{Value: ir.Local(8)}},
								},
							},
						},
					},
				},
			},
		},
	}
	for _, tc := range tests {
		// We do the full setup/teardown for every test, or else we'd get
		// collisions between testcases due to statefulness.
		t.Run(tc.note, func(t *testing.T) {
			resultBL, modified := iropt.BlockTransformPassBlocks(tc.initial, iropt.EmptyLoopReplacement)
			result := resultBL
			if !modified {
				if !tc.expectNotModified {
					t.Fatalf("[%s] expected: modified == true, got: modified == false", tc.note)
				}
			}
			// Check value equality of expected vs actual response.
			if !cmp.Equal(tc.expected, result, cmpopts.IgnoreUnexported(ir.Location{})) {
				t.Logf("[%s] diff:\n%s", tc.note, cmp.Diff(tc.expected, result, cmpopts.IgnoreUnexported(ir.Location{})))
				t.Fatalf("[%s] expected:\n%v\n\ngot:\n%v", tc.note, mustJSON(t, tc.expected), mustJSON(t, result))
			}
		})
	}
}
