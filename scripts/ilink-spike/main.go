package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

var defaultStatePath = filepath.Join(".state", "default.json")

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var err error
	switch os.Args[1] {
	case "login":
		err = runLogin(ctx, os.Args[2:])
	case "listen":
		err = runListen(ctx, os.Args[2:])
	case "send":
		err = runSend(ctx, os.Args[2:])
	case "peers":
		err = runPeers(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.SessionExpired() {
			fmt.Fprintln(os.Stderr, "会话已过期（-14），需要重新 login：", err)
			os.Exit(3)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `用法：
  ilink-spike login  [-state path] [-raw]                扫码登录，记录状态接口耗时
  ilink-spike listen [-state path] [-raw] [-echo]        长轮询收消息，保存 context_token
  ilink-spike send   [-state path] [-to peer] (-text 内容 | -len N [-zh])
  ilink-spike peers  [-state path]                       列出已记录的对端`)
}

func runLogin(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	statePath := fs.String("state", defaultStatePath, "状态文件")
	raw := fs.Bool("raw", false, "打印各接口原始 JSON（token 已打码）")
	_ = fs.Parse(args)

	st, err := loadState(*statePath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*statePath), 0o700); err != nil {
		return err
	}
	c := NewClient(defaultBaseURL, "")
	if *raw {
		c.RawLog = os.Stdout
	}
	for attempt := 1; attempt <= 3; attempt++ {
		qr, err := c.GetBotQRCode(ctx)
		if err != nil {
			return err
		}
		pngPath := filepath.Join(filepath.Dir(*statePath), "qrcode.png")
		if err := qrcode.WriteFile(qr.ImgContent, qrcode.Medium, 320, pngPath); err != nil {
			fmt.Printf("二维码图片生成失败（%v）；qrcode_img_content 长度=%d 前 200 字=%q\n",
				err, len(qr.ImgContent), truncate(qr.ImgContent, 200))
		} else {
			if q, err := qrcode.New(qr.ImgContent, qrcode.Low); err == nil {
				fmt.Println(q.ToSmallString(false))
			}
			fmt.Printf("二维码已写入 %s（终端显示不清时打开该图片），请用微信扫码（第 %d 次）\n", pngPath, attempt)
		}

		res, err := pollQRCode(ctx, c, qr.QRCode)
		if err != nil {
			return err
		}
		if res == nil {
			fmt.Println("二维码已过期，重新获取")
			continue
		}
		return finishLogin(st, *statePath, res)
	}
	return errors.New("二维码连续过期 3 次")
}

// pollQRCode 打印每次调用的时刻与耗时，用于判断 get_qrcode_status 是否为服务端挂起（spec §10 问题 2）。
// 返回 nil, nil 表示二维码过期。
func pollQRCode(ctx context.Context, c *Client, code string) (*QRStatus, error) {
	start := time.Now()
	for {
		t0 := time.Now()
		s, err := c.GetQRCodeStatus(ctx, code)
		if err != nil {
			return nil, err
		}
		took := time.Since(t0)
		fmt.Printf("%s [+%6.1fs] status=%-9s 本次耗时=%5.1fs 客户端超时=%v\n",
			time.Now().Format("15:04:05"), time.Since(start).Seconds(), s.Status, took.Seconds(), s.ClientTimeout)
		switch s.Status {
		case "confirmed":
			return s, nil
		case "expired":
			return nil, nil
		}
		if took < time.Second {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Second):
			}
		}
	}
}

func finishLogin(st *State, path string, s *QRStatus) error {
	if st.BotID != "" {
		fmt.Printf("上次 bot_id=%s，本次 bot_id=%s，同一 bot：%v\n", st.BotID, s.BotID, st.BotID == s.BotID)
		fmt.Printf("上次 owner=%s，本次 owner=%s\n", st.OwnerUserID, s.UserID)
	}
	st.BaseURL = s.BaseURL
	if st.BaseURL == "" {
		st.BaseURL = defaultBaseURL
	}
	st.BotToken = s.BotToken
	st.BotID = s.BotID
	st.OwnerUserID = s.UserID
	st.Cursor = ""
	st.LoginCount++
	st.SavedAt = time.Now()
	fmt.Printf("登录成功：bot_id=%s owner=%s base_url=%s；保留 %d 个旧对端的 context_token（用于问题 3）\n",
		st.BotID, st.OwnerUserID, st.BaseURL, len(st.Peers))
	return st.save(path)
}

func runListen(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("listen", flag.ExitOnError)
	statePath := fs.String("state", defaultStatePath, "状态文件")
	raw := fs.Bool("raw", false, "打印各接口原始 JSON（token 已打码）")
	echo := fs.Bool("echo", false, "收到用户消息时回一条确认")
	_ = fs.Parse(args)

	st, err := loadState(*statePath)
	if err != nil {
		return err
	}
	if st.BotToken == "" {
		return errors.New("状态文件里没有 bot_token，先运行 login")
	}
	c := NewClient(st.BaseURL, st.BotToken)
	if *raw {
		c.RawLog = os.Stdout
	}
	fmt.Printf("开始监听 bot_id=%s owner=%s（Ctrl+C 退出）\n", st.BotID, st.OwnerUserID)
	for ctx.Err() == nil {
		up, err := c.GetUpdates(ctx, st.Cursor)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var apiErr *APIError
			if errors.As(err, &apiErr) {
				if apiErr.SessionExpired() {
					fmt.Fprintf(os.Stderr, "登录（%s）后 %s 出现 -14\n",
						st.SavedAt.Format(time.RFC3339), time.Since(st.SavedAt).Round(time.Second))
				}
				return err
			}
			fmt.Fprintf(os.Stderr, "getupdates 失败（%v），5s 后重试\n", err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(5 * time.Second):
			}
			continue
		}
		for _, m := range up.Msgs {
			fmt.Printf("%s msg_id=%d type=%d from=%s owner=%v group=%q items=%v has_context_token=%v text=%q\n",
				time.Now().Format("15:04:05"), m.MessageID, m.MessageType, m.FromUserID,
				m.FromUserID == st.OwnerUserID, m.GroupID, m.ItemTypes(), m.ContextToken != "", truncate(m.Text(), 80))
			st.recordInbound(m, time.Now())
			if *echo && m.MessageType == messageTypeUser && m.ContextToken != "" {
				if err := c.SendText(ctx, m.FromUserID, m.ContextToken, "spike 收到："+truncate(m.Text(), 40)); err != nil {
					fmt.Fprintln(os.Stderr, "echo 发送失败：", err)
				}
			}
		}
		st.Cursor = up.GetUpdatesBuf
		if err := st.save(*statePath); err != nil {
			return err
		}
	}
	return nil
}

func runSend(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	statePath := fs.String("state", defaultStatePath, "状态文件")
	to := fs.String("to", "", "目标 peer_id，默认扫码人")
	text := fs.String("text", "", "发送内容")
	n := fs.Int("len", 0, "发送恰好 N 个字符的长度测试文本（优先于 -text）")
	zh := fs.Bool("zh", false, "长度测试用中文填充（每字 3 字节）")
	_ = fs.Parse(args)

	st, err := loadState(*statePath)
	if err != nil {
		return err
	}
	if st.BotToken == "" {
		return errors.New("状态文件里没有 bot_token，先运行 login")
	}
	if *to == "" {
		*to = st.OwnerUserID
	}
	peer := st.Peers[*to]
	if peer == nil || peer.ContextToken == "" {
		return fmt.Errorf("没有 %s 的 context_token：让对方先给 bot 发一条消息，并运行 listen 记录", *to)
	}
	body := *text
	if *n > 0 {
		body = buildLengthText(*n, *zh)
	}
	if body == "" {
		return errors.New("需要 -text 或 -len")
	}
	fmt.Printf("发送给 %s：%d 字 / %d 字节；context_token 获取于 %s（当时 bot_id=%s，当前 bot_id=%s）\n",
		*to, len([]rune(body)), len(body), peer.ContextTokenAt.Format(time.RFC3339), peer.BotIDAtToken, st.BotID)
	if err := NewClient(st.BaseURL, st.BotToken).SendText(ctx, *to, peer.ContextToken, body); err != nil {
		return err
	}
	fmt.Println("sendmessage 返回成功。注意：200 不代表已送达，请在微信里确认是否收到、是否完整。")
	return nil
}

func runPeers(args []string) error {
	fs := flag.NewFlagSet("peers", flag.ExitOnError)
	statePath := fs.String("state", defaultStatePath, "状态文件")
	_ = fs.Parse(args)

	st, err := loadState(*statePath)
	if err != nil {
		return err
	}
	fmt.Printf("bot_id=%s owner=%s login_count=%d\n", st.BotID, st.OwnerUserID, st.LoginCount)
	ids := make([]string, 0, len(st.Peers))
	for id := range st.Peers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		p := st.Peers[id]
		age := "-"
		if !p.ContextTokenAt.IsZero() {
			age = time.Since(p.ContextTokenAt).Round(time.Second).String()
		}
		fmt.Printf("%s owner=%v has_token=%v token_age=%s token_bot=%s last=%q\n",
			id, id == st.OwnerUserID, p.ContextToken != "", age, p.BotIDAtToken, p.LastText)
	}
	return nil
}
