package iropt_test

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/open-policy-agent/opa/ir"
	"github.com/styrainc/enterprise-opa-private/pkg/iropt"
)

func mustJSON(t *testing.T, x any) string {
	t.Helper()
	bs, err := json.MarshalIndent(x, "", "\t")
	if err != nil {
		t.Fatal("could not convert to JSON:", x)
		return ""
	}
	return string(bs)
}

func TestLoopInvariantCodeMotion(t *testing.T) {
	tests := []struct {
		note              string
		initial           []*ir.Block
		expected          []*ir.Block
		err               error
		expectNotModified bool
	}{
		{
			note:              "licm on blank block list",
			initial:           []*ir.Block{},
			expected:          []*ir.Block{},
			expectNotModified: true,
		},
		{
			note: "licm on simple loop",
			initial: []*ir.Block{
				{
					Stmts: []ir.Stmt{
						&ir.ScanStmt{
							Source: 3, Key: 4, Value: 5, Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.NopStmt{},
									&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(5)}, Array: 7},
									&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(6)}, Array: 8},
								},
							},
						},
					},
				},
			},
			expected: []*ir.Block{
				{
					Stmts: []ir.Stmt{
						&ir.NopStmt{},
						&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(6)}, Array: 8},
						&ir.ScanStmt{
							Source: 3, Key: 4, Value: 5, Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(5)}, Array: 7},
								},
							},
						},
					},
				},
			},
		},
		{
			note: "licm on basic nested loop, 2-levels",
			initial: []*ir.Block{
				{
					Stmts: []ir.Stmt{
						&ir.ScanStmt{
							Source: 3, Key: 4, Value: 5, Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.ScanStmt{
										Source: 9, Key: 10, Value: 11, Block: &ir.Block{
											Stmts: []ir.Stmt{
												&ir.NopStmt{},
												&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(6)}, Array: 8},
												&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(5)}, Array: 7},
												&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(11)}, Array: 12},
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
						&ir.NopStmt{},
						&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(6)}, Array: 8},
						&ir.ScanStmt{
							Source: 9, Key: 10, Value: 11, Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(11)}, Array: 12},
								},
							},
						},
						&ir.ScanStmt{
							Source: 3, Key: 4, Value: 5, Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(5)}, Array: 7},
								},
							},
						},
					},
				},
			},
		},
		{
			note: "licm on basic nested loop, 3-levels",
			initial: []*ir.Block{
				{
					Stmts: []ir.Stmt{
						&ir.ScanStmt{
							Source: 3, Key: 4, Value: 5, Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.ScanStmt{
										Source: 9, Key: 10, Value: 11, Block: &ir.Block{
											Stmts: []ir.Stmt{
												&ir.NopStmt{},
												&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(6)}, Array: 8},
												&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(5)}, Array: 7},
												&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(11)}, Array: 12},
												&ir.ScanStmt{
													Source: 13, Key: 14, Value: 15, Block: &ir.Block{
														Stmts: []ir.Stmt{
															&ir.MakeNumberIntStmt{Value: 5, Target: 16},
														},
													},
												},
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
						&ir.NopStmt{},
						&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(6)}, Array: 8},
						&ir.MakeNumberIntStmt{Value: 5, Target: 16},
						&ir.ScanStmt{
							Source: 13, Key: 14, Value: 15, Block: &ir.Block{
								Stmts: []ir.Stmt{},
							},
						},
						&ir.ScanStmt{
							Source: 9, Key: 10, Value: 11, Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(11)}, Array: 12},
								},
							},
						},
						&ir.ScanStmt{
							Source: 3, Key: 4, Value: 5, Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(5)}, Array: 7},
								},
							},
						},
					},
				},
			},
		},
		// Simple nesting cases.
		{
			note: "licm on simple loop, nested under a BlockStmt",
			initial: []*ir.Block{
				{
					Stmts: []ir.Stmt{
						&ir.BlockStmt{
							Blocks: []*ir.Block{
								{
									Stmts: []ir.Stmt{
										&ir.ScanStmt{
											Source: 3, Key: 4, Value: 5, Block: &ir.Block{
												Stmts: []ir.Stmt{
													&ir.NopStmt{},
													&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(5)}, Array: 7},
													&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(6)}, Array: 8},
												},
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
										&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(6)}, Array: 8},
										&ir.ScanStmt{
											Source: 3, Key: 4, Value: 5, Block: &ir.Block{
												Stmts: []ir.Stmt{
													&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(5)}, Array: 7},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			note: "licm on simple loop, nested under a NotStmt",
			initial: []*ir.Block{
				{
					Stmts: []ir.Stmt{
						&ir.NotStmt{
							Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.ScanStmt{
										Source: 3, Key: 4, Value: 5, Block: &ir.Block{
											Stmts: []ir.Stmt{
												&ir.NopStmt{},
												&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(5)}, Array: 7},
												&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(6)}, Array: 8},
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
						&ir.NotStmt{
							Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.NopStmt{},
									&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(6)}, Array: 8},
									&ir.ScanStmt{
										Source: 3, Key: 4, Value: 5, Block: &ir.Block{
											Stmts: []ir.Stmt{
												&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(5)}, Array: 7},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			note: "licm on simple loop, nested under a WithStmt",
			initial: []*ir.Block{
				{
					Stmts: []ir.Stmt{
						&ir.WithStmt{
							Local: 90,
							Path:  []int{91, 92, 93},
							Value: ir.Operand{Value: ir.Local(5)},
							Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.ScanStmt{
										Source: 3, Key: 4, Value: 5, Block: &ir.Block{
											Stmts: []ir.Stmt{
												&ir.NopStmt{},
												&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(5)}, Array: 7},
												&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(6)}, Array: 8},
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
						&ir.WithStmt{
							Local: 90,
							Path:  []int{91, 92, 93},
							Value: ir.Operand{Value: ir.Local(5)},
							Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.NopStmt{},
									&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(6)}, Array: 8},
									&ir.ScanStmt{
										Source: 3, Key: 4, Value: 5, Block: &ir.Block{
											Stmts: []ir.Stmt{
												&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(5)}, Array: 7},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		// Nested cases -- unmodified nesting instruction.
		{
			note: "licm on simple loop, containing an unliftable BlockStmt",
			initial: []*ir.Block{
				{
					Stmts: []ir.Stmt{
						&ir.ScanStmt{
							Source: 3, Key: 4, Value: 5, Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.BlockStmt{
										Blocks: []*ir.Block{
											{
												Stmts: []ir.Stmt{
													&ir.NopStmt{},
													&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(5)}, Array: 8},
													&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(4)}, Array: 7},
												},
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
						&ir.ScanStmt{
							Source: 3, Key: 4, Value: 5, Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.BlockStmt{
										Blocks: []*ir.Block{
											{
												Stmts: []ir.Stmt{
													&ir.NopStmt{},
													&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(5)}, Array: 8},
													&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(4)}, Array: 7},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			note: "licm on simple loop, containing an unliftable NotStmt",
			initial: []*ir.Block{
				{
					Stmts: []ir.Stmt{
						&ir.ScanStmt{
							Source: 3, Key: 4, Value: 5, Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.NotStmt{
										Block: &ir.Block{
											Stmts: []ir.Stmt{
												&ir.NopStmt{},
												&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(5)}, Array: 7},
												&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(6)}, Array: 8},
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
						&ir.ScanStmt{
							Source: 3, Key: 4, Value: 5, Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.NotStmt{
										Block: &ir.Block{
											Stmts: []ir.Stmt{
												&ir.NopStmt{},
												&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(5)}, Array: 7},
												&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(6)}, Array: 8},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			note: "licm on simple loop, containing an unliftable WithStmt",
			initial: []*ir.Block{
				{
					Stmts: []ir.Stmt{
						&ir.ScanStmt{
							Source: 3, Key: 4, Value: 5, Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.WithStmt{
										Local: 90,
										Path:  []int{91, 92, 93},
										Value: ir.Operand{Value: ir.Local(5)},
										Block: &ir.Block{
											Stmts: []ir.Stmt{
												&ir.NopStmt{},
												&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(5)}, Array: 7},
												&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(6)}, Array: 8},
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
						&ir.ScanStmt{
							Source: 3, Key: 4, Value: 5, Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.WithStmt{
										Local: 90,
										Path:  []int{91, 92, 93},
										Value: ir.Operand{Value: ir.Local(5)},
										Block: &ir.Block{
											Stmts: []ir.Stmt{
												&ir.NopStmt{},
												&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(5)}, Array: 7},
												&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(6)}, Array: 8},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		// Edge-case: BreakStmt.
		{
			note: "licm on simple loop, with independent statements in a BlockStmt, no-lift for Block",
			initial: []*ir.Block{
				{
					Stmts: []ir.Stmt{
						&ir.ScanStmt{
							Source: 3, Key: 4, Value: 5, Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.NopStmt{},
									&ir.BlockStmt{
										Blocks: []*ir.Block{
											{
												Stmts: []ir.Stmt{
													&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(5)}, Array: 7},
													&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(6)}, Array: 8},
													&ir.BreakStmt{Index: 2},
												},
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
						&ir.NopStmt{},
						&ir.ScanStmt{
							Source: 3, Key: 4, Value: 5, Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.BlockStmt{
										Blocks: []*ir.Block{
											{
												Stmts: []ir.Stmt{
													&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(5)}, Array: 7},
													&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(6)}, Array: 8},
													&ir.BreakStmt{Index: 2},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			note: "licm on simple loop, with independent statements in a BlockStmt, Block liftable",
			initial: []*ir.Block{
				{
					Stmts: []ir.Stmt{
						&ir.ScanStmt{
							Source: 3, Key: 4, Value: 5, Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.NopStmt{},
									&ir.BlockStmt{
										Blocks: []*ir.Block{
											{
												Stmts: []ir.Stmt{
													&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(7)}, Array: 9},
													&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(6)}, Array: 8},
													&ir.BreakStmt{Index: 2},
												},
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
						&ir.NopStmt{},
						&ir.BlockStmt{
							Blocks: []*ir.Block{
								{
									Stmts: []ir.Stmt{
										&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(7)}, Array: 9},
										&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(6)}, Array: 8},
										&ir.BreakStmt{Index: 1},
									},
								},
							},
						},
						&ir.ScanStmt{
							Source: 3, Key: 4, Value: 5, Block: &ir.Block{
								Stmts: []ir.Stmt{},
							},
						},
					},
				},
			},
		},
		{
			note: "licm on simple loop, BreakStmt liftable with rewriting",
			initial: []*ir.Block{
				{
					Stmts: []ir.Stmt{
						&ir.ScanStmt{
							Source: 3, Key: 4, Value: 5, Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.NopStmt{},
									&ir.BreakStmt{Index: 2},
								},
							},
						},
					},
				},
			},
			expected: []*ir.Block{
				{
					Stmts: []ir.Stmt{
						&ir.NopStmt{},
						&ir.BreakStmt{Index: 1},
						&ir.ScanStmt{
							Source: 3, Key: 4, Value: 5, Block: &ir.Block{
								Stmts: []ir.Stmt{},
							},
						},
					},
				},
			},
		},
		{
			note: "licm on simple loop, BreakStmt liftable with deletion",
			initial: []*ir.Block{
				{
					Stmts: []ir.Stmt{
						&ir.ScanStmt{
							Source: 3, Key: 4, Value: 5, Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.NopStmt{},
									&ir.BreakStmt{Index: 0},
								},
							},
						},
					},
				},
			},
			expected: []*ir.Block{
				{
					Stmts: []ir.Stmt{
						&ir.NopStmt{},
						&ir.ScanStmt{
							Source: 3, Key: 4, Value: 5, Block: &ir.Block{
								Stmts: []ir.Stmt{},
							},
						},
					},
				},
			},
		},
		// Edge-case: AssignVarOnceStmt and ObjectInsertOnceStmt.
		// TODO(philip): Update these tests once we know if it's safe to lift loop-invariant *OnceStmt types.
		{
			note: "licm -- no lift for loop-invariant AssignVarOnceStmt or ObjectInsertOnceStmt",
			initial: []*ir.Block{
				{
					Stmts: []ir.Stmt{
						&ir.ScanStmt{
							Source: 3, Key: 4, Value: 5, Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.NopStmt{},
									&ir.AssignVarOnceStmt{Source: ir.Operand{Value: ir.Local(6)}, Target: 8},
									&ir.ObjectInsertOnceStmt{Key: ir.Operand{Value: ir.Local(10)}, Value: ir.Operand{Value: ir.Local(11)}, Object: 9},
								},
							},
						},
					},
				},
			},
			expected: []*ir.Block{
				{
					Stmts: []ir.Stmt{
						&ir.NopStmt{},
						&ir.ScanStmt{
							Source: 3, Key: 4, Value: 5, Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.AssignVarOnceStmt{Source: ir.Operand{Value: ir.Local(6)}, Target: 8},
									&ir.ObjectInsertOnceStmt{Key: ir.Operand{Value: ir.Local(10)}, Value: ir.Operand{Value: ir.Local(11)}, Object: 9},
								},
							},
						},
					},
				},
			},
		},
		// Edge-case: CallStmt and CallDynamicStmt.
		{
			note: "licm -- no lift for loop-invariant CallStmt or CallDynamicStmt",
			initial: []*ir.Block{
				{
					Stmts: []ir.Stmt{
						&ir.ScanStmt{
							Source: 3, Key: 4, Value: 5, Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.NopStmt{},
									&ir.CallStmt{Func: "monty.pythons.flying.circus", Args: []ir.Operand{}, Result: 8},
									&ir.CallDynamicStmt{Args: []ir.Local{}, Result: 9, Path: []ir.Operand{}},
								},
							},
						},
					},
				},
			},
			expected: []*ir.Block{
				{
					Stmts: []ir.Stmt{
						&ir.NopStmt{},
						&ir.ScanStmt{
							Source: 3, Key: 4, Value: 5, Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.CallStmt{Func: "monty.pythons.flying.circus", Args: []ir.Operand{}, Result: 8},
									&ir.CallDynamicStmt{Args: []ir.Local{}, Result: 9, Path: []ir.Operand{}},
								},
							},
						},
					},
				},
			},
		},
		// Intra-loop dependency safety checks.
		{
			note: "licm -- intra-loop dependencies",
			initial: []*ir.Block{
				{
					Stmts: []ir.Stmt{
						&ir.ScanStmt{
							Source: 3, Key: 4, Value: 5, Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.NopStmt{},
									&ir.MakeArrayStmt{Capacity: 10, Target: 7},
									&ir.NopStmt{},
									&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(5)}, Array: 7},
									&ir.AssignVarOnceStmt{Source: ir.Operand{Value: ir.Local(7)}, Target: 9},
								},
							},
						},
					},
				},
			},
			expected: []*ir.Block{
				{
					Stmts: []ir.Stmt{
						&ir.NopStmt{},
						&ir.NopStmt{},
						&ir.ScanStmt{
							Source: 3, Key: 4, Value: 5, Block: &ir.Block{
								Stmts: []ir.Stmt{
									&ir.MakeArrayStmt{Capacity: 10, Target: 7},
									&ir.ArrayAppendStmt{Value: ir.Operand{Value: ir.Local(5)}, Array: 7},
									&ir.AssignVarOnceStmt{Source: ir.Operand{Value: ir.Local(7)}, Target: 9},
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
			resultBL, modified := iropt.LoopPassBlocks(tc.initial, iropt.LoopInvariantCodeMotion)
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
