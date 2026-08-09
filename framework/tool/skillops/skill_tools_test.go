package toolskill

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sixath/framework/skills"
	core "github.com/sixath/framework/tool"
)

func TestRegisterSkillsListViewTools(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "alpha-skill")
	docsDir := filepath.Join(skillDir, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillContent := "---\nname: alpha-skill\ndescription: Alpha skill\ntags: [database]\n---\n# Alpha body"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "guide.md"), []byte("# Guide"), 0o644); err != nil {
		t.Fatal(err)
	}

	betaDir := filepath.Join(dir, "beta-skill")
	if err := os.MkdirAll(betaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(betaDir, "SKILL.md"), []byte("---\nname: beta-skill\ndescription: Beta skill\ntags: [frontend]\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	idx, err := skills.NewIndex([]string{dir}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	reg := core.NewRegistry()
	if err := RegisterSkillsListViewTools(reg, idx, nil); err != nil {
		t.Fatal(err)
	}

	listTool, ok := reg.Get("skills_list")
	if !ok {
		t.Fatal("skills_list not registered")
	}
	allRes, err := listTool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	allMap := allRes.(map[string]any)
	allSkills := allMap["skills"].([]map[string]any)
	if len(allSkills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(allSkills))
	}

	filtered, err := listTool.Execute(context.Background(), map[string]any{"category": "database"})
	if err != nil {
		t.Fatal(err)
	}
	fSkills := filtered.(map[string]any)["skills"].([]map[string]any)
	if len(fSkills) != 1 || fSkills[0]["name"] != "alpha-skill" {
		t.Fatalf("category filter: %#v", fSkills)
	}

	viewTool, ok := reg.Get("skill_view")
	if !ok {
		t.Fatal("skill_view not registered")
	}
	viewRes, err := viewTool.Execute(context.Background(), map[string]any{"name": "alpha-skill"})
	if err != nil {
		t.Fatal(err)
	}
	viewMap := viewRes.(map[string]any)
	if !strings.Contains(viewMap["content"].(string), "Alpha body") {
		t.Fatalf("unexpected content: %v", viewMap["content"])
	}
	linked := viewMap["linked_files"].(map[string][]string)
	if len(linked["docs"]) != 1 || linked["docs"][0] != "docs/guide.md" {
		t.Fatalf("linked_files: %#v", linked)
	}

	fileRes, err := viewTool.Execute(context.Background(), map[string]any{
		"name":      "alpha-skill",
		"file_path": "docs/guide.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	fileMap := fileRes.(map[string]any)
	if fileMap["content"] != "# Guide" {
		t.Fatalf("file content: %q", fileMap["content"])
	}
}

func TestRegisterLoadSkillTool_AndReadSkillFile(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "my-skill")
	docsDir := filepath.Join(skillDir, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	docPath := filepath.Join(docsDir, "extra.md")
	skillContent := "---\nname: my-skill\ndescription: test\n---\n# Body"
	if err := os.WriteFile(skillPath, []byte(skillContent), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := os.WriteFile(docPath, []byte("# Extra doc"), 0o644); err != nil {
		t.Fatalf("write docs/extra.md: %v", err)
	}

	idx, err := skills.NewIndex([]string{dir}, nil, nil)
	if err != nil {
		t.Fatalf("NewIndex: %v", err)
	}
	reg := core.NewRegistry()
	err = RegisterLoadSkillTool(reg, idx, nil)
	if err != nil {
		t.Fatalf("RegisterLoadSkillTool: %v", err)
	}

	list := reg.List()
	names := make(map[string]bool)
	for _, tl := range list {
		names[tl.Name] = true
	}
	if !names["load_skill"] || !names["read_skill_file"] {
		t.Fatalf("expected load_skill and read_skill_file in registry, got %v", names)
	}

	loadTool, _ := reg.Get("load_skill")
	body, err := loadTool.Execute(context.Background(), map[string]any{"name": "my-skill"})
	if err != nil {
		t.Fatalf("load_skill execute: %v", err)
	}
	if body.(string) != skillContent {
		t.Fatalf("load_skill body mismatch, got len=%d", len(body.(string)))
	}

	readTool, _ := reg.Get("read_skill_file")
	content, err := readTool.Execute(context.Background(), map[string]any{"name": "my-skill", "path": "docs/extra.md"})
	if err != nil {
		t.Fatalf("read_skill_file execute: %v", err)
	}
	if content.(string) != "# Extra doc" {
		t.Fatalf("read_skill_file content: %q", content)
	}

	_, err = readTool.Execute(context.Background(), map[string]any{"name": "my-skill", "path": "../other/skip"})
	if err == nil {
		t.Fatal("expected error for .. path in read_skill_file")
	}
}

func TestRegisterExecuteSkillScriptTool_Disabled(t *testing.T) {
	idx, _ := skills.NewIndex(nil, nil, nil)
	reg := core.NewRegistry()
	err := RegisterExecuteSkillScriptTool(reg, idx, false, nil)
	if err != nil {
		t.Fatalf("RegisterExecuteSkillScriptTool: %v", err)
	}
	tl, _ := reg.Get("execute_skill_script")
	_, err = tl.Execute(context.Background(), map[string]any{"name": "any", "path": "scripts/run.sh"})
	if err == nil {
		t.Fatal("expected error when script execution is disabled")
	}
	if err != nil && err.Error() == "" {
		t.Fatalf("expected non-empty error message: %v", err)
	}
}

func TestRegisterExecuteSkillScriptTool_Validation(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "s1")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(path, []byte("---\nname: s1\ndescription: x\n---\n"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	idx, err := skills.NewIndex([]string{dir}, nil, nil)
	if err != nil {
		t.Fatalf("NewIndex: %v", err)
	}
	reg := core.NewRegistry()
	err = RegisterExecuteSkillScriptTool(reg, idx, true, nil)
	if err != nil {
		t.Fatalf("RegisterExecuteSkillScriptTool: %v", err)
	}
	tl, _ := reg.Get("execute_skill_script")
	ctx := context.Background()

	// skill not found
	_, err = tl.Execute(ctx, map[string]any{"name": "nonexistent", "path": "scripts/run.sh"})
	if err == nil {
		t.Fatal("expected error for nonexistent skill")
	}

	// path must be under scripts/
	_, err = tl.Execute(ctx, map[string]any{"name": "s1", "path": "docs/readme.md"})
	if err == nil {
		t.Fatal("expected error for path not under scripts/")
	}

	// path ..
	_, err = tl.Execute(ctx, map[string]any{"name": "s1", "path": "../other/run.sh"})
	if err == nil {
		t.Fatal("expected error for .. path")
	}

	// file not exist
	_, err = tl.Execute(ctx, map[string]any{"name": "s1", "path": "scripts/run.sh"})
	if err == nil {
		t.Fatal("expected error when script file does not exist")
	}
}

func TestRegisterExecuteSkillScriptTool_Success(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "s1")
	scriptsDir := filepath.Join(skillDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	scriptPath := filepath.Join(scriptsDir, "run.sh")
	if err := os.WriteFile(skillPath, []byte("---\nname: s1\ndescription: x\n---\n"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	// echo something so we can assert output
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho OK\n"), 0o755); err != nil {
		t.Fatalf("write scripts/run.sh: %v", err)
	}

	idx, err := skills.NewIndex([]string{dir}, nil, nil)
	if err != nil {
		t.Fatalf("NewIndex: %v", err)
	}
	reg := core.NewRegistry()
	err = RegisterExecuteSkillScriptTool(reg, idx, true, nil)
	if err != nil {
		t.Fatalf("RegisterExecuteSkillScriptTool: %v", err)
	}
	tl, _ := reg.Get("execute_skill_script")
	out, err := tl.Execute(context.Background(), map[string]any{"name": "s1", "path": "scripts/run.sh"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	s := out.(string)
	if s != "OK\n" && s != "OK\r\n" {
		t.Fatalf("expected output OK, got %q", s)
	}
}

func TestRegisterExecuteSkillScriptTool_Args(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "arg-skill")
	scriptsDir := filepath.Join(skillDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	scriptPath := filepath.Join(scriptsDir, "args.sh")
	if err := os.WriteFile(skillPath, []byte("---\nname: arg-skill\ndescription: args\n---\n"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nprintf '%s|%s\\n' \"$1\" \"$2\"\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	idx, err := skills.NewIndex([]string{dir}, nil, nil)
	if err != nil {
		t.Fatalf("NewIndex: %v", err)
	}
	reg := core.NewRegistry()
	if err := RegisterExecuteSkillScriptTool(reg, idx, true, nil); err != nil {
		t.Fatalf("RegisterExecuteSkillScriptTool: %v", err)
	}
	tl, _ := reg.Get("execute_skill_script")
	out, err := tl.Execute(context.Background(), map[string]any{
		"name": "arg-skill",
		"path": "scripts/args.sh",
		"args": []any{"-FlowId", "4_v0cag1d3guo8"},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	got := strings.TrimSpace(out.(string))
	if got != "-FlowId|4_v0cag1d3guo8" {
		t.Fatalf("unexpected args output: %q", got)
	}
}

func TestRegisterExecuteSkillScriptTool_AllowedExtensionsAndTimeout(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "s1")
	scriptsDir := filepath.Join(skillDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	scriptPath := filepath.Join(scriptsDir, "run.sh")
	if err := os.WriteFile(skillPath, []byte("---\nname: s1\ndescription: x\n---\n"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho EXT_OK\n"), 0o755); err != nil {
		t.Fatalf("write run.sh: %v", err)
	}

	idx, _ := skills.NewIndex([]string{dir}, nil, nil)
	reg := core.NewRegistry()
	opts := &ExecuteSkillScriptOptions{
		AllowedExtensions: []string{".sh"},
		TimeoutSeconds:    10,
	}
	err := RegisterExecuteSkillScriptTool(reg, idx, true, opts)
	if err != nil {
		t.Fatalf("RegisterExecuteSkillScriptTool: %v", err)
	}
	tl, _ := reg.Get("execute_skill_script")
	out, err := tl.Execute(context.Background(), map[string]any{"name": "s1", "path": "scripts/run.sh"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if out.(string) != "EXT_OK\n" && out.(string) != "EXT_OK\r\n" {
		t.Fatalf("unexpected output: %q", out)
	}

	// .py not in list when only .sh
	reg2 := core.NewRegistry()
	_ = RegisterExecuteSkillScriptTool(reg2, idx, true, opts)
	tl2, _ := reg2.Get("execute_skill_script")
	pyPath := filepath.Join(scriptsDir, "x.py")
	_ = os.WriteFile(pyPath, []byte("print(1)"), 0o644)
	_, err = tl2.Execute(context.Background(), map[string]any{"name": "s1", "path": "scripts/x.py"})
	if err == nil {
		t.Fatal("expected error when extension .py not in allowed list")
	}
}

// TestRegisterExecuteSkillScriptTool_PythonScript 验证 .py 脚本使用 python3 解释器执行（当本机有 python3 时）。
func TestRegisterExecuteSkillScriptTool_PythonScript(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "py-skill")
	scriptsDir := filepath.Join(skillDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	pyPath := filepath.Join(scriptsDir, "echo.py")
	if err := os.WriteFile(skillPath, []byte("---\nname: py-skill\ndescription: py\n---\n"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := os.WriteFile(pyPath, []byte("#!/usr/bin/env python3\nprint('PYOK')\n"), 0o644); err != nil {
		t.Fatalf("write echo.py: %v", err)
	}

	idx, _ := skills.NewIndex([]string{dir}, nil, nil)
	reg := core.NewRegistry()
	opts := &ExecuteSkillScriptOptions{
		AllowedExtensions: []string{".sh", ".py"},
		TimeoutSeconds:    5,
	}
	err := RegisterExecuteSkillScriptTool(reg, idx, true, opts)
	if err != nil {
		t.Fatalf("RegisterExecuteSkillScriptTool: %v", err)
	}
	tl, _ := reg.Get("execute_skill_script")
	out, err := tl.Execute(context.Background(), map[string]any{"name": "py-skill", "path": "scripts/echo.py"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	s := out.(string)
	if strings.Contains(s, "PYOK") {
		return // python3 可用且执行成功
	}
	// 本机无 python3（如 Windows 未加入 PATH）时跳过，不视为失败
	if strings.Contains(s, "exit status") || strings.Contains(s, "9009") || strings.Contains(s, "not found") {
		t.Skip("python3 not available, skipping .py script test")
	}
	t.Fatalf("expected output containing PYOK from python3, got %q", s)
}
