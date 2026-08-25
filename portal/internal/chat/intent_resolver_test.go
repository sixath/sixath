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
	bound = append(bound, FamilyRCA, FamilyCode)
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
	if _, ok := set[FamilyCode]; ok {
		t.Fatal("code must not be active for gitlab-only query")
	}
}

func TestIntentResolver_CodeAnalysisActivatesCodeNotRCA(t *testing.T) {
	r := IntentResolver{}
	bound := []string{FamilyCore, FamilyCode, FamilyRCA, "mcp:gitlab"}
	res := r.Resolve(context.Background(), IntentResolveInput{
		UserText:      "根据代码分析 存档迁移整体流程梳理",
		BoundFamilies: bound,
	})
	set := familySet(res.ActiveFamilies)
	if _, ok := set[FamilyCode]; !ok {
		t.Fatalf("code must be active, got %v source=%s reason=%s", res.ActiveFamilies, res.Source, res.Reason)
	}
	if _, ok := set[FamilyRCA]; ok {
		t.Fatalf("rca must not activate for code-flow question, got %v", res.ActiveFamilies)
	}
	if _, ok := set["mcp:gitlab"]; ok {
		t.Fatal("gitlab must not activate")
	}
}

func TestIntentResolver_JaegerUnionsCodeWhenBound(t *testing.T) {
	r := IntentResolver{}
	bound := []string{FamilyCore, FamilyCode, FamilyRCA}
	res := r.Resolve(context.Background(), IntentResolveInput{
		UserText:      "看下这条 Jaeger trace",
		BoundFamilies: bound,
	})
	set := familySet(res.ActiveFamilies)
	if _, ok := set[FamilyRCA]; !ok {
		t.Fatalf("rca missing: %v", res.ActiveFamilies)
	}
	if _, ok := set[FamilyCode]; !ok {
		t.Fatalf("code should be unioned: %v", res.ActiveFamilies)
	}
}

func TestIntentResolver_JaegerDoesNotInventCode(t *testing.T) {
	r := IntentResolver{}
	bound := []string{FamilyCore, FamilyRCA}
	res := r.Resolve(context.Background(), IntentResolveInput{
		UserText:      "看下这条 Jaeger trace",
		BoundFamilies: bound,
	})
	set := familySet(res.ActiveFamilies)
	if _, ok := set[FamilyCode]; ok {
		t.Fatal("must not invent code family")
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
	if _, ok := set[FamilyCore]; !ok {
		t.Fatalf("want core, got %v", res.ActiveFamilies)
	}
	if _, ok := set[FamilyRCA]; !ok {
		t.Fatalf("primary rca should remain on fail_narrow, got %v", res.ActiveFamilies)
	}
	if _, ok := set["mcp:gitlab"]; ok {
		t.Fatalf("gitlab must not activate, got %v", res.ActiveFamilies)
	}
}

func TestIntentResolver_CodePathQuestionKeepsCodeDropsData(t *testing.T) {
	r := IntentResolver{}
	bound := []string{FamilyCore, FamilyCode, FamilyData, FamilySkills, FamilyMemory}
	res := r.Resolve(context.Background(), IntentResolveInput{
		UserText:      "区域侧有用户信息了，union在注册的时候会发生什么",
		BoundFamilies: bound,
	})
	if res.Source != "fail_narrow" {
		t.Fatalf("source=%s reason=%s", res.Source, res.Reason)
	}
	set := familySet(res.ActiveFamilies)
	if _, ok := set[FamilyCode]; !ok {
		t.Fatalf("want code primary, got %v", res.ActiveFamilies)
	}
	if _, ok := set[FamilyData]; ok {
		t.Fatalf("data must not activate, got %v", res.ActiveFamilies)
	}
	if _, ok := set[FamilySkills]; ok {
		t.Fatalf("skills must not activate, got %v", res.ActiveFamilies)
	}
	if _, ok := set[FamilyMemory]; ok {
		t.Fatalf("memory must not activate, got %v", res.ActiveFamilies)
	}
}

func TestIntentResolver_ExplicitMongoActivatesData(t *testing.T) {
	r := IntentResolver{}
	bound := []string{FamilyCore, FamilyCode, FamilyData}
	res := r.Resolve(context.Background(), IntentResolveInput{
		UserText:      "查 mongo 里有没有这个用户",
		BoundFamilies: bound,
	})
	if res.Source != "rules" {
		t.Fatalf("source=%s reason=%s active=%v", res.Source, res.Reason, res.ActiveFamilies)
	}
	set := familySet(res.ActiveFamilies)
	if _, ok := set[FamilyData]; !ok {
		t.Fatalf("want data, got %v", res.ActiveFamilies)
	}
	if _, ok := set[FamilyCode]; ok {
		t.Fatalf("unique data hit must not add code, got %v", res.ActiveFamilies)
	}
}
