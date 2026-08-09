package executor

import "testing"

func TestMaskLiterals(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"SELECT 1", "SELECT 1"},
		{"SELECT * FROM users WHERE token='abc123'", "SELECT * FROM users WHERE token='***'"},
		{"INSERT INTO logs VALUES (1, 'msg', 'secret')", "INSERT INTO logs VALUES (1, '***', '***')"},
		{"SELECT 'it''s fine'", "SELECT '***'"},
	}
	for _, tt := range tests {
		got := MaskLiterals(tt.in)
		if got != tt.want {
			t.Errorf("MaskLiterals(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
