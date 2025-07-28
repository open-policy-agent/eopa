package lia_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/open-policy-agent/eopa/pkg/lia"
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

	t.Run("String", func(t *testing.T) {
		if exp, act := "<db report>", rep.String(); exp != act {
			t.Errorf("expected string %q, got %q", exp, act)
		}
	})

	t.Run("Count", func(t *testing.T) {
		if exp, act := 2, rep.Count(ctx); exp != act {
			t.Errorf("expected count %d, got %d", exp, act)
		}
	})

	t.Run("ToJSON", func(t *testing.T) {
		exp := `[{"node_id":"26537362-84d6-454b-839a-20eaf4e4efc0","req_id":6,"decision_id":"0921342f-7bb2-4b7f-a8d4-bf6b8375f9be","value_a":1,"value_b":1,"input":true,"path":"test\/p","eval_ns_a":96880,"eval_ns_b":52297047},` +
			`{"node_id":"26537362-84d6-454b-839a-20eaf4e4efc0","req_id":7,"decision_id":"3d26e0c1-a9b0-4db8-a934-9df987c5ce9a","value_a":1,"value_b":1,"input":"whatever","path":"test\/p","eval_ns_a":113279,"eval_ns_b":50245047}]` + "\n"
		buf := bytes.Buffer{}
		if err := rep.ToJSON(ctx, &buf); err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(exp, buf.String()); diff != "" {
			t.Errorf("unexpected result: (-want, +got):\n%s", diff)
		}
	})

	t.Run("ToCSV", func(t *testing.T) {
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
	})

	t.Run("ToPretty", func(t *testing.T) {
		exp := `┌───────────────────────────────────────┬────────┬───────────────────────────────────────┬─────────┬─────────┬───────────┬────────┬─────────────────┬─────────────────┐
│                node_id                │ req_id │              decision_id              │ value_a │ value_b │   input   │  path  │ eval_duration_a │ eval_duration_b │
├───────────────────────────────────────┼────────┼───────────────────────────────────────┼─────────┼─────────┼───────────┼────────┼─────────────────┼─────────────────┤
│  26537362-84d6-454b-839a-20eaf4e4efc0 │      6 │  0921342f-7bb2-4b7f-a8d4-bf6b8375f9be │       1 │       1 │      true │ test/p │         96.88µs │     52.297047ms │
│  26537362-84d6-454b-839a-20eaf4e4efc0 │      7 │  3d26e0c1-a9b0-4db8-a934-9df987c5ce9a │       1 │       1 │  whatever │ test/p │       113.279µs │     50.245047ms │
└───────────────────────────────────────┴────────┴───────────────────────────────────────┴─────────┴─────────┴───────────┴────────┴─────────────────┴─────────────────┘
`

		buf := bytes.Buffer{}
		if err := rep.ToPretty(ctx, &buf); err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(exp, buf.String()); diff != "" {
			t.Errorf("unexpected result: (-want, +got):\n%s", diff)
		}
	})

	t.Run("Grouped/ToCSV", func(t *testing.T) {
		input := `{"node_id":"26537362-84d6-454b-839a-20eaf4e4efc0","req_id":6,"decision_id":"0921342f-7bb2-4b7f-a8d4-bf6b8375f9be","value_a":1,"value_b":1,"input":true,"path":"test/p","eval_ns_a":96880,"eval_ns_b":52297047}
{"node_id":"26537362-84d6-454b-839a-20eaf4e4efc0","req_id":7,"decision_id":"3d26e0c1-a9b0-4db8-a934-9df987c5ce9a","value_a":1,"value_b":1,"input":"whatever","path":"test/p","eval_ns_a":113279,"eval_ns_b":50245047}
{"node_id":"26537362-84d6-454b-839a-20eaf4e4efc1","req_id":8,"decision_id":"3d26e0c1-a9b0-4db8-a934-9df987c5ce9b","value_a":2,"value_b":3,"input":"whatever","path":"test/p","eval_ns_a":1132790,"eval_ns_b":502450470}
`
		r := strings.NewReader(input)
		rep, err := lia.ReportFromReader(ctx, r, lia.Grouped(true))
		if err != nil {
			t.Fatal(err)
		}
		exp := `path,input,n,mean_primary_ns,median_primary_ns,min_primary_ns,max_primary_ns,stddev_primary_ns,var_primary_ns,mean_secondary_ns,median_secondary_ns,min_secondary_ns,max_secondary_ns,stddev_secondary_ns,var_secondary_ns
test/p,whatever,2,623034.5,623034.5,113279,1132790,720903.1415942783,519701339560.5,276347758.5,276347758.5,50245047,502450470,319757521.09263116,102244872295304460
test/p,true,1,96880,96880,96880,96880,,,52297047,52297047,52297047,52297047,,
`
		buf := bytes.Buffer{}
		if err := rep.ToCSV(ctx, &buf); err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(exp, buf.String()); diff != "" {
			t.Errorf("unexpected result: (-want, +got):\n%s", diff)
		}
	})

	t.Run("Grouped/ToPretty", func(t *testing.T) {
		input := `{"node_id":"26537362-84d6-454b-839a-20eaf4e4efc0","req_id":6,"decision_id":"0921342f-7bb2-4b7f-a8d4-bf6b8375f9be","value_a":1,"value_b":1,"input":true,"path":"test/p","eval_ns_a":96880,"eval_ns_b":52297047}
{"node_id":"26537362-84d6-454b-839a-20eaf4e4efc0","req_id":7,"decision_id":"3d26e0c1-a9b0-4db8-a934-9df987c5ce9a","value_a":1,"value_b":1,"input":"whatever","path":"test/p","eval_ns_a":113279,"eval_ns_b":50245047}
{"node_id":"26537362-84d6-454b-839a-20eaf4e4efc1","req_id":8,"decision_id":"3d26e0c1-a9b0-4db8-a934-9df987c5ce9b","value_a":2,"value_b":3,"input":"whatever","path":"test/p","eval_ns_a":1132790,"eval_ns_b":502450470}
`
		r := strings.NewReader(input)
		rep, err := lia.ReportFromReader(ctx, r, lia.Grouped(true))
		if err != nil {
			t.Fatal(err)
		}
		exp := `┌────────┬───────────┬───┬───────────────────────┬─────────────────────────┬──────────────────────┬──────────────────────┬─────────────────────────┬──────────────────────┬─────────────────────────┬───────────────────────────┬────────────────────────┬────────────────────────┬───────────────────────────┬────────────────────────┐
│  path  │   input   │ n │ mean_primary_duration │ median_primary_duration │ min_primary_duration │ max_primary_duration │ stddev_primary_duration │ var_primary_duration │ mean_secondary_duration │ median_secondary_duration │ min_secondary_duration │ max_secondary_duration │ stddev_secondary_duration │ var_secondary_duration │
├────────┼───────────┼───┼───────────────────────┼─────────────────────────┼──────────────────────┼──────────────────────┼─────────────────────────┼──────────────────────┼─────────────────────────┼───────────────────────────┼────────────────────────┼────────────────────────┼───────────────────────────┼────────────────────────┤
│ test/p │  whatever │ 2 │             623.034µs │               623.034µs │            113.279µs │            1.13279ms │               720.903µs │       8m39.70133956s │            276.347758ms │              276.347758ms │            50.245047ms │            502.45047ms │              319.757521ms │ 28401h21m12.295304464s │
│ test/p │      true │ 1 │               96.88µs │                 96.88µs │              96.88µs │              96.88µs │                      0s │                   0s │             52.297047ms │               52.297047ms │            52.297047ms │            52.297047ms │                        0s │                     0s │
└────────┴───────────┴───┴───────────────────────┴─────────────────────────┴──────────────────────┴──────────────────────┴─────────────────────────┴──────────────────────┴─────────────────────────┴───────────────────────────┴────────────────────────┴────────────────────────┴───────────────────────────┴────────────────────────┘
`
		buf := bytes.Buffer{}
		if err := rep.ToPretty(ctx, &buf); err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(exp, buf.String()); diff != "" {
			t.Errorf("unexpected result: (-want, +got):\n%s", diff)
		}
	})

	t.Run("Limit/ToCSV", func(t *testing.T) {
		input := `{"node_id":"26537362-84d6-454b-839a-20eaf4e4efc0","req_id":6,"decision_id":"0921342f-7bb2-4b7f-a8d4-bf6b8375f9be","value_a":1,"value_b":1,"input":true,"path":"test/p","eval_ns_a":96880,"eval_ns_b":52297047}
{"node_id":"26537362-84d6-454b-839a-20eaf4e4efc0","req_id":7,"decision_id":"3d26e0c1-a9b0-4db8-a934-9df987c5ce9a","value_a":1,"value_b":1,"input":"whatever","path":"test/p","eval_ns_a":113279,"eval_ns_b":50245047}
{"node_id":"26537362-84d6-454b-839a-20eaf4e4efc1","req_id":8,"decision_id":"3d26e0c1-a9b0-4db8-a934-9df987c5ce9b","value_a":2,"value_b":3,"input":"whatever","path":"test/p","eval_ns_a":1132790,"eval_ns_b":502450470}
`
		r := strings.NewReader(input)
		rep, err := lia.ReportFromReader(ctx, r, lia.Limit(2))
		if err != nil {
			t.Fatal(err)
		}
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
	})

	t.Run("Limit+Grouped/ToCSV", func(t *testing.T) {
		input := `{"node_id":"26537362-84d6-454b-839a-20eaf4e4efc0","req_id":6,"decision_id":"0921342f-7bb2-4b7f-a8d4-bf6b8375f9be","value_a":1,"value_b":1,"input":true,"path":"test/p","eval_ns_a":96880,"eval_ns_b":52297047}
{"node_id":"26537362-84d6-454b-839a-20eaf4e4efc0","req_id":7,"decision_id":"3d26e0c1-a9b0-4db8-a934-9df987c5ce9a","value_a":1,"value_b":1,"input":"whatever","path":"test/p","eval_ns_a":113279,"eval_ns_b":50245047}
{"node_id":"26537362-84d6-454b-839a-20eaf4e4efc1","req_id":8,"decision_id":"3d26e0c1-a9b0-4db8-a934-9df987c5ce9b","value_a":2,"value_b":3,"input":"whatever","path":"test/p","eval_ns_a":1132790,"eval_ns_b":502450470}
`
		r := strings.NewReader(input)
		rep, err := lia.ReportFromReader(ctx, r, lia.Limit(1), lia.Grouped(true))
		if err != nil {
			t.Fatal(err)
		}
		exp := `path,input,n,mean_primary_ns,median_primary_ns,min_primary_ns,max_primary_ns,stddev_primary_ns,var_primary_ns,mean_secondary_ns,median_secondary_ns,min_secondary_ns,max_secondary_ns,stddev_secondary_ns,var_secondary_ns
test/p,whatever,2,623034.5,623034.5,113279,1132790,720903.1415942783,519701339560.5,276347758.5,276347758.5,50245047,502450470,319757521.09263116,102244872295304460
`
		buf := bytes.Buffer{}
		if err := rep.ToCSV(ctx, &buf); err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(exp, buf.String()); diff != "" {
			t.Errorf("unexpected result: (-want, +got):\n%s", diff)
		}
	})
}
