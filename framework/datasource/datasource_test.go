package datasource

import (
	"encoding/json"
	"testing"
)

func TestConfigFromMap_PoolFields(t *testing.T) {
	tests := []struct {
		name string
		in   map[string]interface{}
		want Config
	}{
		{
			name: "all numeric types tolerated",
			in: map[string]interface{}{
				"id":                    "ds1",
				"type":                  "mysql",
				"max_open_conns":        100,
				"max_idle_conns":        float64(20),
				"conn_max_lifetime_sec": json.Number("3600"),
			},
			want: Config{
				ID:              "ds1",
				Type:            "mysql",
				MaxOpenConns:    100,
				MaxIdleConns:    20,
				ConnMaxLifetime: 3600,
			},
		},
		{
			name: "missing pool fields stay zero",
			in:   map[string]interface{}{"id": "ds1", "type": "mysql"},
			want: Config{ID: "ds1", Type: "mysql"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConfigFromMap(tt.in)
			if got.MaxOpenConns != tt.want.MaxOpenConns ||
				got.MaxIdleConns != tt.want.MaxIdleConns ||
				got.ConnMaxLifetime != tt.want.ConnMaxLifetime {
				t.Errorf("pool fields = (%d,%d,%d), want (%d,%d,%d)",
					got.MaxOpenConns, got.MaxIdleConns, got.ConnMaxLifetime,
					tt.want.MaxOpenConns, tt.want.MaxIdleConns, tt.want.ConnMaxLifetime)
			}
		})
	}
}
