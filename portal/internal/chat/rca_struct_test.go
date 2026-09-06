package chat

import (
	"testing"

	"google.golang.org/protobuf/types/known/structpb"
)

func mustRCAStruct(t *testing.T, funcPath string, extra map[string]any) *structpb.Struct {
	t.Helper()
	rca := map[string]any{"func_path": funcPath}
	for k, v := range extra {
		rca[k] = v
	}
	st, err := structpb.NewStruct(map[string]any{"rca": rca})
	if err != nil {
		t.Fatal(err)
	}
	return st
}
