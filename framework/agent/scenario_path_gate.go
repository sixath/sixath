package agent

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/sixath/framework/tool"
)

const scenarioPathGateSoftPrompt = `用户问题里的场景对不上 control_flow 路径：当前分支走不到这些调用/写库，不能说成会发生。
可以保留报告排版。请按 rca_read.control_flow 的 when 改结论：该场景命中的路径没有这些 side effect，应写「跳过/不会执行/不写入」，或明确它们只在其它 when 上发生。

Scenario/path mismatch:
%s`

var (
	scenarioNumberRE = regexp.MustCompile(`\b(\d+)\b`)
	condParseRE      = regexp.MustCompile(`(?i)([A-Za-z_][A-Za-z0-9_]*)\s*(==|!=)\s*([A-Za-z_][A-Za-z0-9_]*|\d+)`)
	dbWriteClaimRE   = regexp.MustCompile(`写入本地映射|写入映射|写映射表|回填映射|插入映射|落库|写库|写入本地|写入[^。\n]{0,40}映射|will write|persist(?:s|ed)?(?:\s+to)?`)
)

var existingUserAliases = []string{
	"已有用户", "区域已有", "用户已存在", "already exists", "already registered", "user already",
}

var writeCallMarkers = []string{
	"Insert", "Write", "Save", "Create", "Update", "Delete", "Persist", "Store", "Put",
}

// EvaluateScenarioPathGate pins the user-question scenario onto control_flow
// paths. Unreachable writes/calls must not be claimed as happening.
// No matching scenario in the question → allow (do not invent 1105).
func EvaluateScenarioPathGate(userQuestion, finalText string, sources []CodeQuoteSource) EvidenceGateResult {
	facts := extractScenarioFacts(userQuestion)
	if !facts.active() || len(sources) == 0 {
		return EvidenceGateResult{Allow: true}
	}
	var missing []string
	for _, src := range sources {
		for _, fn := range src.ControlFlow {
			missing = append(missing, scenarioMismatchesForFunc(facts, finalText, fn)...)
		}
	}
	if len(missing) == 0 {
		return EvidenceGateResult{Allow: true}
	}
	return EvidenceGateResult{
		Allow:  false,
		Action: "inject",
		Reason: "scenario path mismatch",
		Prompt: fmt.Sprintf(scenarioPathGateSoftPrompt, strings.Join(uniqueStrings(missing), "\n")),
	}
}

type scenarioFacts struct {
	numbers       []int
	existingUser  bool
	hasZero       bool
	hasNonZero    bool
}

func (f scenarioFacts) active() bool {
	return len(f.numbers) > 0 || f.existingUser
}

func extractScenarioFacts(question string) scenarioFacts {
	var f scenarioFacts
	q := strings.TrimSpace(question)
	if q == "" {
		return f
	}
	lower := strings.ToLower(q)
	for _, a := range existingUserAliases {
		if strings.Contains(q, a) || strings.Contains(lower, strings.ToLower(a)) {
			f.existingUser = true
			break
		}
	}
	seen := map[int]struct{}{}
	for _, m := range scenarioNumberRE.FindAllString(q, -1) {
		n, err := strconv.Atoi(m)
		if err != nil {
			continue
		}
		if skipScenarioNumber(n) {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		f.numbers = append(f.numbers, n)
		if n == 0 {
			f.hasZero = true
		} else {
			f.hasNonZero = true
		}
	}
	return f
}

func skipScenarioNumber(n int) bool {
	if n == 0 {
		return false
	}
	if n < 100 || n > 99999 {
		return true
	}
	if n >= 1900 && n <= 2100 {
		return true
	}
	return false
}

func scenarioMismatchesForFunc(facts scenarioFacts, final string, fn tool.ControlFlowFunc) []string {
	selected := selectScenarioPaths(fn.Paths, facts)
	if len(selected) == 0 || len(selected) == len(fn.Paths) {
		return nil
	}
	unreachable := unreachableCalls(fn.Paths, selected)
	var writes []string
	for _, name := range unreachable {
		if looksLikeWriteCall(name) {
			writes = append(writes, name)
		}
	}
	if len(writes) == 0 && !claimsNamedUnreachable(final, unreachable) {
		return nil
	}
	if answerHasSkipPhrase(final) {
		return nil
	}
	var missing []string
	for _, name := range unreachable {
		exclusive := exclusiveWhenForCall(fn, name)
		if answerAcknowledgesCFG(final, name, exclusive, fn) {
			continue
		}
		named := strings.Contains(final, name)
		writeHit := looksLikeWriteCall(name) && claimsDBWrite(final)
		if !named && !writeHit {
			continue
		}
		when := strings.Join(exclusive, "; ")
		missing = append(missing, fmt.Sprintf("%s: %s is unreachable under this scenario (when %s)",
			fn.File, name, when))
	}
	return missing
}

func selectScenarioPaths(paths []tool.ControlFlowPath, facts scenarioFacts) []tool.ControlFlowPath {
	if len(paths) == 0 || !facts.active() {
		return nil
	}
	pinZero := facts.hasZero && !facts.hasNonZero && !facts.existingUser
	pinNonZero := (facts.hasNonZero || facts.existingUser) && !facts.hasZero
	if !pinZero && !pinNonZero {
		return nil
	}
	var out []tool.ControlFlowPath
	for _, p := range paths {
		if pinZero && pathMatchesZero(p, paths) {
			out = append(out, p)
			continue
		}
		if pinNonZero && pathMatchesNonZero(p, paths, facts) {
			out = append(out, p)
		}
	}
	return out
}

func pathMatchesZero(p tool.ControlFlowPath, all []tool.ControlFlowPath) bool {
	for _, w := range p.When {
		c, ok := parseWhenCond(w)
		if !ok {
			continue
		}
		if c.op == "==" && c.value == "0" && identHasZeroSplit(c.ident, all) {
			return true
		}
	}
	return false
}

func pathMatchesNonZero(p tool.ControlFlowPath, all []tool.ControlFlowPath, facts scenarioFacts) bool {
	for _, w := range p.When {
		c, ok := parseWhenCond(w)
		if !ok {
			continue
		}
		if c.op == "==" {
			n, err := strconv.Atoi(c.value)
			if err == nil && n != 0 && numberIn(facts.numbers, n) {
				return true
			}
			if facts.existingUser && err == nil && n != 0 {
				return true
			}
		}
		if c.op == "!=" && c.value == "0" && identHasZeroSplit(c.ident, all) {
			return true
		}
	}
	return false
}

type whenCond struct {
	ident string
	op    string
	value string
}

func parseWhenCond(when string) (whenCond, bool) {
	m := condParseRE.FindStringSubmatch(strings.TrimSpace(when))
	if len(m) != 4 {
		return whenCond{}, false
	}
	return whenCond{ident: m[1], op: m[2], value: m[3]}, true
}

func identHasZeroSplit(ident string, paths []tool.ControlFlowPath) bool {
	eq0, ne0 := false, false
	for _, p := range paths {
		for _, w := range p.When {
			c, ok := parseWhenCond(w)
			if !ok || !strings.EqualFold(c.ident, ident) {
				continue
			}
			if c.op == "==" && c.value == "0" {
				eq0 = true
			}
			if c.op == "!=" && c.value == "0" {
				ne0 = true
			}
		}
	}
	return eq0 && ne0
}

func unreachableCalls(all, selected []tool.ControlFlowPath) []string {
	inSelected := map[string]struct{}{}
	for _, p := range selected {
		for _, c := range p.Calls {
			inSelected[c] = struct{}{}
		}
	}
	order := make([]string, 0)
	seen := map[string]struct{}{}
	for _, p := range all {
		for _, c := range p.Calls {
			if _, ok := inSelected[c]; ok {
				continue
			}
			if _, ok := seen[c]; ok {
				continue
			}
			seen[c] = struct{}{}
			order = append(order, c)
		}
	}
	return order
}

func looksLikeWriteCall(name string) bool {
	for _, m := range writeCallMarkers {
		if strings.Contains(name, m) {
			return true
		}
	}
	return false
}

func claimsDBWrite(final string) bool {
	return dbWriteClaimRE.FindStringIndex(final) != nil
}

func claimsNamedUnreachable(final string, names []string) bool {
	for _, n := range names {
		if strings.Contains(final, n) {
			return true
		}
	}
	return false
}

func answerHasSkipPhrase(final string) bool {
	for _, p := range gatedCallSkipPhrases {
		if strings.Contains(final, p) {
			return true
		}
	}
	return false
}

func numberIn(nums []int, n int) bool {
	for _, x := range nums {
		if x == n {
			return true
		}
	}
	return false
}
