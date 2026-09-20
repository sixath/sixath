package chat

import (
	"fmt"
	"strings"
	"testing"

	"github.com/sixath/framework/model"
)

func TestClassifyAttachment(t *testing.T) {
	if k, ok := ClassifyAttachment("a.PNG", "image/png"); !ok || k != KindImage {
		t.Fatalf("png: %v %v", k, ok)
	}
	if k, ok := ClassifyAttachment("a.log", "text/plain"); !ok || k != KindText {
		t.Fatalf("log: %v %v", k, ok)
	}
	if _, ok := ClassifyAttachment("a.pdf", "application/pdf"); ok {
		t.Fatal("pdf should reject")
	}
	// extension wins over mismatched MIME
	if k, ok := ClassifyAttachment("a.png", "application/pdf"); !ok || k != KindImage {
		t.Fatalf("ext wins: %v %v", k, ok)
	}
	if _, ok := ClassifyAttachment("a.pdf", "image/png"); ok {
		t.Fatal("pdf ext should reject even with image mime")
	}
	// no extension: MIME fallback
	if k, ok := ClassifyAttachment("screenshot", "image/png"); !ok || k != KindImage {
		t.Fatalf("mime image fallback: %v %v", k, ok)
	}
	if k, ok := ClassifyAttachment("log", "text/plain"); !ok || k != KindText {
		t.Fatalf("mime text fallback: %v %v", k, ok)
	}
	// reject svg / arbitrary image/*
	if _, ok := ClassifyAttachment("icon", "image/svg+xml"); ok {
		t.Fatal("svg mime should reject")
	}
	if _, ok := ClassifyAttachment("x", "image/bmp"); ok {
		t.Fatal("bmp mime should reject")
	}
	if k, ok := ClassifyAttachment("shot", "image/jpeg; charset=binary"); !ok || k != KindImage {
		t.Fatalf("jpeg with params: %v %v", k, ok)
	}
}

func TestDefaultMIMEForKind(t *testing.T) {
	if got := DefaultMIMEForKind(KindImage, "a.PNG"); got != "image/png" {
		t.Fatalf("png: %q", got)
	}
	if got := DefaultMIMEForKind(KindImage, "a.jpeg"); got != "image/jpeg" {
		t.Fatalf("jpeg: %q", got)
	}
	if got := DefaultMIMEForKind(KindText, "a.log"); got != "text/plain" {
		t.Fatalf("log: %q", got)
	}
	if got := DefaultMIMEForKind(KindText, "a.json"); got != "application/json" {
		t.Fatalf("json: %q", got)
	}
}

func TestSafeFilename(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{``, ``},
		{`.`, ``},
		{`..`, ``},
		{`../../x.png`, `x.png`},
		{`..\..\x.png`, `x.png`},
		{`/tmp/foo/bar.log`, `bar.log`},
	}
	for _, tc := range cases {
		got := SafeFilename(tc.in)
		if got != tc.want {
			t.Fatalf("SafeFilename(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if strings.Contains(got, "..") {
			t.Fatalf("SafeFilename(%q) contains ..: %q", tc.in, got)
		}
	}
}

func TestBuildUserContentWithAttachments(t *testing.T) {
	// persist=true：始终含图片脚注（落库 / 降级重试）
	persisted := BuildUserContentWithAttachments("hello", []AttachmentMeta{
		{Kind: KindImage, RelativePath: "sessions/s1/uploads/att_1_a.png"},
		{Kind: KindText, RelativePath: "sessions/s1/uploads/att_2_a.log"},
	}, ContentForPersist)
	if !strings.Contains(persisted, "att_1_a.png") || !strings.Contains(persisted, "att_2_a.log") {
		t.Fatalf("persist: %q", persisted)
	}
	// forModelWithParts：可省略图片脚注，文本脚注仍在
	forModel := BuildUserContentWithAttachments("hello", []AttachmentMeta{
		{Kind: KindImage, RelativePath: "sessions/s1/uploads/att_1_a.png"},
		{Kind: KindText, RelativePath: "sessions/s1/uploads/att_2_a.log"},
	}, ContentForModelWithImageParts)
	if strings.Contains(forModel, "〔附件 image〕") {
		t.Fatalf("model with parts should omit image footnote: %q", forModel)
	}
	if !strings.Contains(forModel, "att_2_a.log") {
		t.Fatalf("text footnote required: %q", forModel)
	}
	onlyFootnotes := BuildUserContentWithAttachments("", []AttachmentMeta{
		{Kind: KindText, RelativePath: "sessions/s1/uploads/att_2_a.log"},
	}, ContentForPersist)
	if !strings.Contains(onlyFootnotes, "att_2_a.log") {
		t.Fatalf("footnotes only: %q", onlyFootnotes)
	}
	if strings.HasPrefix(onlyFootnotes, "\n") {
		t.Fatalf("footnotes only should not lead with blank line: %q", onlyFootnotes)
	}
}

func TestBuildImageParts(t *testing.T) {
	const rel = "sessions/s1/uploads/att_1_a.png"
	parts := BuildImageParts([]AttachmentMeta{
		{Kind: KindImage, RelativePath: rel, Mime: "image/png"},
		{Kind: KindText, RelativePath: "sessions/s1/uploads/att_2_a.log"},
	}, func(path string) ([]byte, error) {
		if path != rel {
			return nil, fmt.Errorf("unexpected path %q", path)
		}
		return []byte{0x89, 0x50, 0x4e, 0x47}, nil
	})
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0].Type != model.ContentTypeImageURL {
		t.Fatalf("type: %q", parts[0].Type)
	}
	if !strings.HasPrefix(parts[0].URL, "data:image/png;base64,") {
		t.Fatalf("url: %q", parts[0].URL)
	}
}

func TestBuildImagePartsReadFailure(t *testing.T) {
	parts := BuildImageParts([]AttachmentMeta{
		{Kind: KindImage, RelativePath: "sessions/s1/uploads/missing.png", Mime: "image/png"},
	}, func(string) ([]byte, error) {
		return nil, fmt.Errorf("read failed")
	})
	if len(parts) != 0 {
		t.Fatalf("expected no parts on read failure, got %d", len(parts))
	}
}

func TestUniqueNonEmpty(t *testing.T) {
	got := UniqueNonEmpty([]string{" a ", "", "a", "b", " b "})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %#v", got)
	}
}

func TestAssembleTurnMessages_OnlyCurrentHasParts(t *testing.T) {
	history := []model.Message{
		{Role: "user", Content: "old with footnote 〔附件 image〕sessions/s1/uploads/old.png", Parts: []model.ContentPart{
			{Type: model.ContentTypeImageURL, URL: "data:image/png;base64,OLD"},
		}},
		{Role: "assistant", Content: "ok"},
	}
	current := model.Message{
		Role:    "user",
		Content: "new",
		Parts: []model.ContentPart{
			{Type: model.ContentTypeImageURL, URL: "data:image/png;base64,NEW"},
		},
	}
	msgs := AssembleTurnMessages(history, current)
	if len(msgs) != 3 {
		t.Fatalf("len=%d", len(msgs))
	}
	if len(msgs[0].Parts) != 0 {
		t.Fatalf("history user must not keep Parts: %+v", msgs[0].Parts)
	}
	if msgs[0].Content == "" || !strings.Contains(msgs[0].Content, "old.png") {
		t.Fatalf("history content should keep footnote text: %q", msgs[0].Content)
	}
	if len(msgs[2].Parts) != 1 || msgs[2].Parts[0].URL != "data:image/png;base64,NEW" {
		t.Fatalf("current user Parts: %+v", msgs[2].Parts)
	}
}
