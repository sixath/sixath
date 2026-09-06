package harness

// ResponseChunk 为流式 Agent 响应的一个增量。
type ResponseChunk struct {
	Delta string
	Done  bool
	Err   error
}
