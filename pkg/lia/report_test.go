package lia_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/styrainc/load-private/pkg/lia"
)

func TestReportOutputs(t *testing.T) {
	ctx := context.Background()
	input := `{"node_id":"26537362-84d6-454b-839a-20eaf4e4efc0","req_id":6,"decision_id":"0921342f-7bb2-4b7f-a8d4-bf6b8375f9be","value_a":1,"value_b":1,"input":true,"path":"test/p","eval_ns_a":96880,"eval_ns_b":52297047}
{"node_id":"26537362-84d6-454b-839a-20eaf4e4efc0","req_id":7,"decision_id":"3d26e0c1-a9b0-4db8-a934-9df987c5ce9a","value_a":1,"value_b":1,"input":"whatever","path":"test/p","eval_ns_a":113279,"eval_ns_b":50245047}
`
	r := strings.NewReader(input)

	rep, err := lia.ReportFromReader(ctx, r)
	if err != nil {
		t.Fatal(err)
	}

	if exp, act := "<db report>", rep.String(); exp != act {
		t.Errorf("expected string %q, got %q", exp, act)
	}

	{
		exp := `[{"node_id":"26537362-84d6-454b-839a-20eaf4e4efc0","req_id":6,"decision_id":"0921342f-7bb2-4b7f-a8d4-bf6b8375f9be","value_a":1,"value_b":1,"input":true,"path":"test\/p","eval_ns_a":96880,"eval_ns_b":52297047},` +
			`{"node_id":"26537362-84d6-454b-839a-20eaf4e4efc0","req_id":7,"decision_id":"3d26e0c1-a9b0-4db8-a934-9df987c5ce9a","value_a":1,"value_b":1,"input":"whatever","path":"test\/p","eval_ns_a":113279,"eval_ns_b":50245047}]` + "\n"
		buf := bytes.Buffer{}
		if err := rep.ToJSON(ctx, &buf); err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(exp, buf.String()); diff != "" {
			t.Errorf("unexpected result: (-want, +got):\n%s", diff)
		}
	}
	{
		exp := `node_id,req_id,decision_id,value_a,value_b,input,path,eval_ns_a,eval_ns_b
26537362-84d6-454b-839a-20eaf4e4efc0,6,0921342f-7bb2-4b7f-a8d4-bf6b8375f9be,1,1,true,test/p,96880,52297047
26537362-84d6-454b-839a-20eaf4e4efc0,7,3d26e0c1-a9b0-4db8-a934-9df987c5ce9a,1,1,whatever,test/p,113279,50245047
`

		buf := bytes.Buffer{}
		if err := rep.ToCSV(ctx, &buf); err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(exp, buf.String()); diff != "" {
			t.Errorf("unexpected result: (-want, +got):\n%s", diff)
		}
	}

	{
		exp := `┌───────────────────────────────────────┬────────┬───────────────────────────────────────┬─────────┬─────────┬───────────┬────────┬───────────┬───────────┐
│                node_id                │ req_id │              decision_id              │ value_a │ value_b │   input   │  path  │ eval_ns_a │ eval_ns_b │
├───────────────────────────────────────┼────────┼───────────────────────────────────────┼─────────┼─────────┼───────────┼────────┼───────────┼───────────┤
│ 26537362-84d6-454b-839a-20eaf4e4efc0  │      6 │ 0921342f-7bb2-4b7f-a8d4-bf6b8375f9be  │       1 │       1 │   true    │ test/p │     96880 │  52297047 │
│ 26537362-84d6-454b-839a-20eaf4e4efc0  │      7 │ 3d26e0c1-a9b0-4db8-a934-9df987c5ce9a  │       1 │       1 │ whatever  │ test/p │    113279 │  50245047 │
└───────────────────────────────────────┴────────┴───────────────────────────────────────┴─────────┴─────────┴───────────┴────────┴───────────┴───────────┘
`

		buf := bytes.Buffer{}
		if err := rep.ToPretty(ctx, &buf); err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(exp, buf.String()); diff != "" {
			t.Errorf("unexpected result: (-want, +got):\n%s", diff)
		}
	}
}
