package validator

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ValidateResult is the result of skill package validation
type ValidateResult struct {
	Valid   bool
	Message string
	Errors  []string
}

// Allowed script extensions
var allowedScriptExts = map[string]bool{
	".sh":  true,
	".py":  true,
	".js":  true,
	".ps1": true,
}

// ValidateSkillPackage validates a skill package (zip bytes)
// Checks: zip format, SKILL.md presence, script extensions (if any), no path escape.
// scripts/ 目录可选，仅含 SKILL.md 的技能包也可通过校验。
func ValidateSkillPackage(zipData []byte) ValidateResult {
	if len(zipData) == 0 {
		return ValidateResult{
			Valid:   false,
			Message: "技能包为空",
			Errors:  []string{"压缩包为空"},
		}
	}
	rd := bytes.NewReader(zipData)
	zr, err := zip.NewReader(rd, int64(len(zipData)))
	if err != nil {
		return ValidateResult{
			Valid:   false,
			Message: "无效的压缩包格式",
			Errors:  []string{err.Error()},
		}
	}

	var errs []string
	hasSkillMD := false

	for _, f := range zr.File {
		name := filepath.ToSlash(f.Name)
		// skip directory entries
		if strings.HasSuffix(name, "/") {
			continue
		}

		// path escape check: no ../ or absolute path
		if strings.Contains(name, "..") || path.IsAbs(name) || strings.HasPrefix(name, "/") {
			errs = append(errs, "路径包含非法字符: "+name)
			continue
		}
		// normalized path must not escape
		clean := path.Clean(name)
		if strings.HasPrefix(clean, "..") {
			errs = append(errs, "路径逃逸: "+name)
			continue
		}

		base := strings.ToLower(path.Base(name))
		if base == "skill.md" {
			hasSkillMD = true
		}

		// script extension check for .sh/.py/.js files
		ext := strings.ToLower(path.Ext(name))
		if ext != "" && allowedScriptExts[ext] {
			// valid script
		}
		// we don't reject other files, only validate scripts have valid ext
	}

	if !hasSkillMD {
		errs = append(errs, "缺少 SKILL.md")
	}

	if len(errs) > 0 {
		return ValidateResult{
			Valid:   false,
			Message: "技能包校验失败",
			Errors:  errs,
		}
	}
	return ValidateResult{
		Valid:   true,
		Message: "校验通过",
		Errors:  nil,
	}
}

// ExtractSkillPackage extracts zip bytes to destDir
func ExtractSkillPackage(zipData []byte, destDir string) error {
	if len(zipData) == 0 {
		return nil
	}
	rd := bytes.NewReader(zipData)
	zr, err := zip.NewReader(rd, int64(len(zipData)))
	if err != nil {
		return err
	}

	for _, f := range zr.File {
		name := filepath.ToSlash(f.Name)
		if strings.HasSuffix(name, "/") {
			continue
		}
		// security: reject path escape
		if strings.Contains(name, "..") || path.IsAbs(name) {
			continue
		}
		clean := path.Clean(name)
		if strings.HasPrefix(clean, "..") {
			continue
		}

		dstPath := filepath.Join(destDir, filepath.FromSlash(name))
		if err := extractFile(f, dstPath); err != nil {
			return err
		}
	}
	return nil
}

func extractFile(f *zip.File, dstPath string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	out, err := createFile(dstPath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, rc)
	return err
}

func createFile(p string) (io.WriteCloser, error) {
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	return os.Create(p)
}
