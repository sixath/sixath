package tooldata

import (
	"context"
	"slices"
	"testing"

	core "github.com/sixath/framework/tool"
)

func TestTableToolsDatasourceIDCandidates(t *testing.T) {
	dsReg := newStubDSRegistry(t, map[string]string{"mysql1": "mysql"})
	store := primedStore(t, dsReg, "mysql1", vmAssignSchema())

	r := core.NewRegistry()
	if err := RegisterDescribeTableTool(r, &DescribeTableConfig{Store: store, Registry: dsReg, DefaultDatasourceID: "mysql1"}); err != nil {
		t.Fatal(err)
	}
	if err := RegisterListTablesTool(r, &ListTablesConfig{Store: store, Registry: dsReg, DefaultDatasourceID: "mysql1"}); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		tool   string
		params map[string]any
	}{
		{"describe_table", map[string]any{"table_name": "vm_assign"}},
		{"list_tables", map[string]any{}},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			tl, ok := r.Get(tc.tool)
			if !ok {
				t.Fatal("not registered")
			}
			bad := map[string]any{"datasource_id": "mysq1"}
			for k, v := range tc.params {
				bad[k] = v
			}
			_, err := tl.Execute(context.Background(), bad)
			e := wantInvalidArg(t, err, core.KeywordOneOf)
			if !slices.Contains(e.Candidates, "mysql1") {
				t.Fatalf("candidates=%v", e.Candidates)
			}

			good := map[string]any{"datasource_id": "default"}
			for k, v := range tc.params {
				good[k] = v
			}
			if _, err := tl.Execute(context.Background(), good); err != nil {
				t.Fatalf("default: %v", err)
			}
		})
	}
}
