package chat

import (
	"context"
	"strings"

	"backend/internal/biz"
)

// FamilyClassifier selects active tool families when rules are ambiguous (Task 3: model impl).
type FamilyClassifier interface {
	Classify(ctx context.Context, userText string, bound, candidates []string) (selected []string, confidence string, err error)
}

type IntentResolveInput struct {
	UserText        string
	BoundFamilies   []string
	PrimaryFamilies []string // optional; empty → InferPrimaryFamilies(BoundFamilies)
	Servers         []*biz.McpServerMeta
	Classifier      FamilyClassifier
}

type IntentResolveResult struct {
	ActiveFamilies []string
	Confidence     string // high | low
	Source         string // rules | classifier | fail_narrow
	Candidates     []string
	Reason         string
}

type IntentResolver struct {
	Classifier FamilyClassifier
}

func (r IntentResolver) Resolve(ctx context.Context, in IntentResolveInput) IntentResolveResult {
	bound := familySet(in.BoundFamilies)
	if len(bound) == 0 {
		bound[FamilyCore] = struct{}{}
	}
	scores := scoreFamilies(in.UserText, bound, in.Servers)
	var hits []string
	for id, sc := range scores {
		if sc > 0 {
			hits = append(hits, id)
		}
	}
	ensureCore := func(ids []string) []string {
		s := familySet(ids)
		s[FamilyCore] = struct{}{}
		out := make([]string, 0, len(s))
		for id := range s {
			if _, ok := bound[id]; ok || id == FamilyCore {
				out = append(out, id)
			}
		}
		return out
	}

	clf := r.Classifier
	if in.Classifier != nil {
		clf = in.Classifier
	}

	var res IntentResolveResult
	switch {
	case len(hits) == 1:
		res = IntentResolveResult{
			ActiveFamilies: ensureCore(hits),
			Confidence:     "high",
			Source:         "rules",
			Candidates:     hits,
			Reason:         "unique_rule_hit",
		}
	case len(hits) == 0:
		res = r.classifyOrNarrow(ctx, in.UserText, bound, nil, clf, "no_rule_hit", in.PrimaryFamilies)
	default:
		res = r.classifyOrNarrow(ctx, in.UserText, bound, hits, clf, "multi_rule_hit", in.PrimaryFamilies)
	}
	res.ActiveFamilies = unionCodeWhenRCA(res.ActiveFamilies, in.BoundFamilies)
	return res
}

func (r IntentResolver) classifyOrNarrow(ctx context.Context, user string, bound map[string]struct{}, candidates []string, clf FamilyClassifier, reason string, primary []string) IntentResolveResult {
	boundList := make([]string, 0, len(bound))
	for id := range bound {
		boundList = append(boundList, id)
	}
	if clf != nil {
		selected, conf, err := clf.Classify(ctx, user, boundList, candidates)
		if err == nil && conf == "high" && len(selected) > 0 {
			clean := filterBoundOnly(selected, bound)
			if len(clean) > 0 {
				return IntentResolveResult{
					ActiveFamilies: withCore(clean),
					Confidence:     "high",
					Source:         "classifier",
					Candidates:     candidates,
					Reason:         reason + ":classifier_ok",
				}
			}
		}
	}
	if len(primary) == 0 {
		primary = InferPrimaryFamilies(boundList)
	}
	narrow := filterBoundOnly(mergeFamilyIDs(candidates, primary...), bound)
	if len(narrow) == 0 {
		narrow = []string{FamilyCore}
	} else {
		narrow = withCore(narrow)
	}
	return IntentResolveResult{
		ActiveFamilies: narrow,
		Confidence:     "low",
		Source:         "fail_narrow",
		Candidates:     candidates,
		Reason:         reason + ":fail_narrow",
	}
}

func scoreFamilies(user string, bound map[string]struct{}, servers []*biz.McpServerMeta) map[string]int {
	toks := tokenizeForOverlap(user)
	scores := map[string]int{}
	lower := strings.ToLower(user)
	for fam, kws := range familyKeywords {
		if _, ok := bound[fam]; !ok {
			continue
		}
		for _, kw := range kws {
			kw = strings.ToLower(kw)
			if kw == "" {
				continue
			}
			if strings.Contains(lower, kw) {
				scores[fam]++
				continue
			}
			if _, ok := toks[kw]; ok {
				scores[fam]++
			}
		}
	}
	for _, s := range servers {
		if s == nil || s.ID == "" {
			continue
		}
		fid := MCPFamilyID(s.ID)
		if _, ok := bound[fid]; !ok {
			continue
		}
		for _, tip := range []string{s.ID, s.Name} {
			tip = strings.ToLower(strings.TrimSpace(tip))
			if tip == "" {
				continue
			}
			if strings.Contains(lower, tip) {
				scores[fid] += 2
			}
			if _, ok := toks[tip]; ok {
				scores[fid] += 2
			}
		}
	}
	for fid := range bound {
		if strings.HasPrefix(fid, "mcp:legacy:") {
			name := strings.TrimPrefix(fid, "mcp:legacy:")
			if name != "" && strings.Contains(lower, strings.ToLower(name)) {
				scores[fid]++
			}
		}
	}
	return scores
}

func withCore(ids []string) []string {
	s := familySet(ids)
	s[FamilyCore] = struct{}{}
	out := make([]string, 0, len(s))
	for id := range s {
		out = append(out, id)
	}
	return out
}

// unionCodeWhenRCA adds FamilyCode when RCA is active and code is bound (trace → source).
func unionCodeWhenRCA(active, bound []string) []string {
	a, b := familySet(active), familySet(bound)
	if _, rca := a[FamilyRCA]; !rca {
		return active
	}
	if _, has := b[FamilyCode]; !has {
		return active
	}
	a[FamilyCode] = struct{}{}
	out := make([]string, 0, len(a))
	for id := range a {
		out = append(out, id)
	}
	return out
}

func filterBoundOnly(ids []string, bound map[string]struct{}) []string {
	var out []string
	for _, id := range ids {
		if _, ok := bound[id]; ok {
			out = append(out, id)
		}
	}
	return out
}
