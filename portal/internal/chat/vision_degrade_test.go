package chat

import (
	"errors"
	"testing"

	"github.com/sixath/framework/model"
)

func TestShouldDegradeVisionError(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New(""), false},
		{errors.New("rate limited"), false},
		{errors.New("model does not support image input"), true},
		{errors.New("VISION capability missing"), true},
		{errors.New("Unsupported modality: image"), true},
		{errors.New("this model does Not Support multimodal"), true},
		{errors.New("IMAGE_URL not allowed"), true},
	}
	for _, tc := range cases {
		got := ShouldDegradeVisionError(tc.err)
		if got != tc.want {
			t.Fatalf("ShouldDegradeVisionError(%v)=%v want %v", tc.err, got, tc.want)
		}
	}
}

func TestVisionDegradeState_RetryOnceWithPersistContent(t *testing.T) {
	persist := "see this\n\n〔附件 image〕sessions/s1/uploads/a.png"
	msgs := []model.Message{
		{Role: "user", Content: "older", Parts: nil},
		{
			Role:    "user",
			Content: "see this",
			Parts:   []model.ContentPart{{Type: model.ContentTypeImageURL, URL: "data:image/png;base64,xx"}},
		},
	}
	st := &VisionDegradeState{PersistContent: persist, HadImageParts: true}

	// Non-vision error: no retry
	if retry, _ := st.TryDegrade(errors.New("timeout"), msgs); retry {
		t.Fatal("timeout should not degrade")
	}
	if st.Degraded() {
		t.Fatal("state should stay clean after non-vision error")
	}

	// Vision error: first retry with persist + Parts=nil
	retry, next := st.TryDegrade(errors.New("modality not supported"), msgs)
	if !retry {
		t.Fatal("expected vision degrade retry")
	}
	if !st.Degraded() {
		t.Fatal("expected degraded flag")
	}
	if len(next) != 2 {
		t.Fatalf("len=%d", len(next))
	}
	last := next[len(next)-1]
	if last.Content != persist {
		t.Fatalf("content=%q want persist footnotes", last.Content)
	}
	if len(last.Parts) != 0 {
		t.Fatalf("Parts must be nil on degrade, got %d", len(last.Parts))
	}
	// Original messages untouched
	if len(msgs[1].Parts) == 0 {
		t.Fatal("original messages mutated")
	}

	// Second vision error: never retry again
	retry, next = st.TryDegrade(errors.New("vision still broken"), next)
	if retry || next != nil {
		t.Fatalf("second retry must not happen: retry=%v next=%v", retry, next)
	}
}

func TestVisionDegradeState_NoPartsSkip(t *testing.T) {
	st := &VisionDegradeState{PersistContent: "x", HadImageParts: false}
	msgs := []model.Message{{Role: "user", Content: "hi"}}
	if retry, _ := st.TryDegrade(errors.New("no vision"), msgs); retry {
		t.Fatal("no parts → no degrade")
	}
}

func TestApplyVisionDegrade(t *testing.T) {
	msgs := []model.Message{
		{Role: "assistant", Content: "ok"},
		{Role: "user", Content: "q", Parts: []model.ContentPart{{Type: model.ContentTypeImageURL, URL: "data:x"}}},
	}
	out := ApplyVisionDegrade(msgs, "q\n\n〔附件 image〕p.png")
	if out[1].Content != "q\n\n〔附件 image〕p.png" || len(out[1].Parts) != 0 {
		t.Fatalf("got %+v", out[1])
	}
	if len(msgs[1].Parts) == 0 {
		t.Fatal("input mutated")
	}
}
