package chat

import (
	"context"
	"testing"

	"backend/internal/biz"
)

func TestIntentResolver_RulesUniqueGitLab(t *testing.T) {
	r := IntentResolver{}
	servers := []*biz.McpServerMeta{{ID: "gitlab", Name: "GitLab"}}
	bound := BoundFamiliesFrom(nil, servers, false, false)
	bound = append(bound, FamilyRCA)
	res := r.Resolve(context.Background(), IntentResolveInput{
		UserText:      "帮我查一下 GitLab 上有哪些项目",
		BoundFamilies: bound,
		Servers:       servers,
	})
	if res.Source != "rules" || res.Confidence != "high" {
		t.Fatalf("got source=%s conf=%s reason=%s", res.Source, res.Confidence, res.Reason)
	}
	set := familySet(res.ActiveFamilies)
	if _, ok := set[FamilyCore]; !ok {
		t.Fatalf("active=%v", res.ActiveFamilies)
	}
	if _, ok := set["mcp:gitlab"]; !ok {
		t.Fatalf("active=%v", res.ActiveFamilies)
	}
	if _, ok := set[FamilyRCA]; ok {
		t.Fatal("rca must not be active for gitlab-only query")
	}
}

func TestIntentResolver_RulesMultiIntentNeedsClassifierOrUnionPath(t *testing.T) {
	r := IntentResolver{Classifier: nil}
	servers := []*biz.McpServerMeta{{ID: "gitlab", Name: "GitLab"}}
	bound := append(BoundFamiliesFrom(nil, servers, false, false), FamilyRCA)
	res := r.Resolve(context.Background(), IntentResolveInput{
		UserText:      "GitLab 部署失败，看下 Jaeger trace",
		BoundFamilies: bound,
		Servers:       servers,
	})
	if res.Source != "fail_narrow" && res.Source != "classifier" {
		t.Fatalf("source=%s", res.Source)
	}
	set := familySet(res.ActiveFamilies)
	if _, ok := set["mcp:gitlab"]; !ok {
		t.Fatalf("expected both families in active via candidates, got %v candidates=%v", res.ActiveFamilies, res.Candidates)
	}
	if _, ok := set[FamilyRCA]; !ok {
		t.Fatalf("expected both families in active via candidates, got %v candidates=%v", res.ActiveFamilies, res.Candidates)
	}
}

func TestIntentResolver_NoHitFailNarrowCoreOnly(t *testing.T) {
	r := IntentResolver{}
	bound := []string{FamilyCore, FamilyRCA, "mcp:gitlab"}
	res := r.Resolve(context.Background(), IntentResolveInput{
		UserText:      "你好",
		BoundFamilies: bound,
	})
	if res.Source != "fail_narrow" {
		t.Fatalf("source=%s", res.Source)
	}
	set := familySet(res.ActiveFamilies)
	if len(set) != 1 {
		t.Fatalf("want only core, got %v", res.ActiveFamilies)
	}
	if _, ok := set[FamilyCore]; !ok {
		t.Fatalf("want only core, got %v", res.ActiveFamilies)
	}
}
