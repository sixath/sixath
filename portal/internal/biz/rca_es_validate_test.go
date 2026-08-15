package biz

import "testing"

func TestValidateRCAESLogConfig(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, endpoint, dsID string
		wantErr              bool
	}{
		{"both", "http://es:9200", "es-logs", true},
		{"neither", "", "", true},
		{"endpoint_only", "http://es:9200", "", false},
		{"ds_only", "", "es-logs", false},
		{"trim_spaces_both", "  ", "  ", true},
		{"endpoint_trim", "  http://es:9200  ", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRCAESLogConfig(tc.endpoint, tc.dsID)
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected: %v", err)
			}
		})
	}
}
