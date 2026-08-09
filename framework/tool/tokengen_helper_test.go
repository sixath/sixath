package tool

// fakeTokenGen 是 TokenGenerator 的测试替身，供 ask_user 等核心工具测试共用。
type fakeTokenGen struct {
	next string
	err  error
}

func (f *fakeTokenGen) NewToken() (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.next, nil
}
