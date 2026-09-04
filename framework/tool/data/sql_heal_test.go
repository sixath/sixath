package tooldata

import (
	"fmt"
	"strings"
	"testing"

	"github.com/sixath/framework/executor"
	"github.com/sixath/framework/metadata"
)

func schemaErr(msg string) error {
	return &executor.SchemaRelatedError{Err: fmt.Errorf("executor: query: %s", msg)}
}

func vmSchema() *metadata.Schema {
	return &metadata.Schema{
		Name: "d_1000_game_virtual_machine_info",
		Tables: []metadata.Table{
			{
				Name: "t_game_virtual_machine_info",
				Columns: []metadata.Column{
					{Name: "vmid"}, {Name: "mgr_ipv4_address"}, {Name: "flow_id"},
					{Name: "assign_state"}, {Name: "name"}, {Name: "area_type"},
				},
			},
			{
				Name: "t_game_virtual_machine_extend_info",
				Columns: []metadata.Column{
					{Name: "vmid"}, {Name: "flow_id"}, {Name: "exec_username"},
				},
			},
			{
				Name: "t_game_virtual_machine_info_test",
				Columns: []metadata.Column{
					{Name: "vmid"}, {Name: "flow_id"}, {Name: "mgr_ipv4_address"},
				},
			},
		},
	}
}

func TestHealReadSQL_dropsUnknownSelectColumn(t *testing.T) {
	sql := "SELECT vmid, mgr_ipv4_address, ecn_id FROM t_game_virtual_machine_info WHERE vmid = 9076"
	err := schemaErr("Error 1054 (42S22): Unknown column 'ecn_id' in 'field list'")
	got, note, ok := HealReadSQL(sql, err, vmSchema())
	if !ok {
		t.Fatalf("expected heal, note=%s", note)
	}
	if strings.Contains(strings.ToLower(got), "ecn_id") {
		t.Fatalf("ecn_id should be dropped: %s", got)
	}
	if !strings.Contains(got, "mgr_ipv4_address") || !strings.Contains(got, "vmid = 9076") {
		t.Fatalf("kept columns/where lost: %s", got)
	}
}

func TestHealReadSQL_doesNotDropWhereColumn(t *testing.T) {
	sql := "SELECT vmid FROM t_game_virtual_machine_info WHERE ecn_id = 1"
	err := schemaErr("Error 1054 (42S22): Unknown column 'ecn_id' in 'where clause'")
	_, _, ok := HealReadSQL(sql, err, vmSchema())
	if ok {
		t.Fatal("must not rewrite WHERE predicates")
	}
}

func TestHealReadSQL_schemaNameUsedAsTable(t *testing.T) {
	sql := "SELECT * FROM d_1000_game_virtual_machine_info WHERE flow_id = '9999_zjvplfx19vdv'"
	err := schemaErr("Error 1146 (42S02): Table 'd_1000_game_virtual_machine_info.d_1000_game_virtual_machine_info' doesn't exist")
	got, note, ok := HealReadSQL(sql, err, vmSchema())
	if !ok {
		t.Fatalf("expected heal, note=%s", note)
	}
	if !strings.Contains(got, "t_game_virtual_machine_info") {
		t.Fatalf("want primary VM table, got %s note=%s", got, note)
	}
	if strings.Contains(got, "t_game_virtual_machine_info_test") {
		t.Fatalf("must not pick _test table: %s", got)
	}
	if !strings.Contains(got, "9999_zjvplfx19vdv") {
		t.Fatalf("where clause lost: %s", got)
	}
}

func TestHealReadSQL_qualifiedSchemaTableStripped(t *testing.T) {
	sql := "SELECT vmid FROM d_1000_game_virtual_machine_info.t_missing WHERE vmid = 1"
	err := schemaErr("Error 1146 (42S02): Table 'd_1000_game_virtual_machine_info.t_missing' doesn't exist")
	_, _, ok := HealReadSQL(sql, err, vmSchema())
	if ok {
		t.Fatal("unknown real table must not be silently rewritten")
	}
}

func TestHealReadSQL_unknownTableErrorListsCandidates(t *testing.T) {
	sql := "SELECT * FROM t_flow WHERE flow_id = 'x'"
	err := schemaErr("Error 1146 (42S02): Table 'd_1000_game_virtual_machine_info.t_flow' doesn't exist")
	note := SchemaHealHint(sql, err, vmSchema())
	if !strings.Contains(note, "t_game_virtual_machine_info") {
		t.Fatalf("hint should list tables with flow_id, got %s", note)
	}
	if !strings.Contains(note, "t_flow") {
		t.Fatalf("hint should mention missing table, got %s", note)
	}
}
