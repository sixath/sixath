package wecom

import "encoding/json"

const (
	CmdSubscribe   = "aibot_subscribe"
	CmdPing        = "ping"
	CmdMsgCallback = "aibot_msg_callback"
	CmdRespondMsg  = "aibot_respond_msg"

	// MaxStreamContentBytes is the WeCom stream content byte limit.
	MaxStreamContentBytes = 20480

	DefaultWSURL = "wss://openws.work.weixin.qq.com"
)

// Frame is a WeCom long-connection JSON frame.
type Frame struct {
	Cmd     string          `json:"cmd,omitempty"`
	Headers FrameHeaders    `json:"headers"`
	Body    json.RawMessage `json:"body,omitempty"`
	// ErrCode/ErrMsg appear at top level on subscribe/respond acks (official docs).
	ErrCode int    `json:"errcode,omitempty"`
	ErrMsg  string `json:"errmsg,omitempty"`
}

// FrameHeaders carries per-frame metadata.
type FrameHeaders struct {
	ReqID string `json:"req_id"`
}

// SubscribeBody is the body for aibot_subscribe.
type SubscribeBody struct {
	BotID  string `json:"bot_id"`
	Secret string `json:"secret"`
}

// SubscribeAckBody is the subscribe response body.
type SubscribeAckBody struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

// RespondMsgBody is the body for aibot_respond_msg.
type RespondMsgBody struct {
	MsgType string         `json:"msgtype"`
	Stream  StreamPayload  `json:"stream"`
}

// StreamPayload is the stream payload inside respond_msg.
type StreamPayload struct {
	ID      string `json:"id"`
	Finish  bool   `json:"finish"`
	Content string `json:"content"`
}
