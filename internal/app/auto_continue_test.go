package app

import (
	"encoding/json"
	"strings"
	"testing"
)

// mkRespLine 拼一行真实的 wrb.fr 响应，供 extractResponseText 解析。
// inner 故意留够槽位：解析层有 len(line)<200 的粗过滤，太短的骨架会被当成噪音。
func mkRespLine(text string) string {
	inner := make([]interface{}, 40)
	inner[1] = []interface{}{"c_1", "r_1"}
	inner[4] = []interface{}{[]interface{}{"rc_1", []interface{}{text}}}
	ib, _ := json.Marshal(inner)
	line, _ := json.Marshal([]interface{}{[]interface{}{"wrb.fr", nil, string(ib)}})
	return string(line)
}

func TestLooksIncomplete(t *testing.T) {
	long := strings.Repeat("这是一段很长的回答内容", 80) // 960 rune
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"空", "", false},
		{"短回答无标点", "好的", false},
		{"长回答句号收尾", long + "。", false},
		{"长回答英文句号收尾", long + ".", false},
		{"长回答逗号收尾", long + "，", true},
		{"长回答冒号收尾", long + "：", true},
		{"代码块没闭合", long + "\n```go\nfunc main() {", true},
		{"代码块已闭合", long + "\n```go\nfunc main() {}\n```", false},
		{"行内代码没闭合", long + " 见 `foo", true},
	}
	for _, c := range cases {
		if got := looksIncomplete(c.text); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestJoinContinuation(t *testing.T) {
	// 续写重复了上一段的结尾：只保留新增部分。
	base := "第一段内容。第二段还在写"
	add := "第二段还在写完了。"
	if got := joinContinuation(base, add); got != "第一段内容。第二段还在写完了。" {
		t.Errorf("重叠去重: got %q", got)
	}

	// 没有重叠：直接接上。
	if got := joinContinuation("abc，", "def。"); got != "abc，def。" {
		t.Errorf("无重叠: got %q", got)
	}

	// 上一段以句末标点收尾：补换行，避免两段粘在一起。
	if got := joinContinuation("第一段。", "第二段。"); got != "第一段。\n第二段。" {
		t.Errorf("句末补换行: got %q", got)
	}

	// 模型从头重写：不拼接，避免整段翻倍。
	head := strings.Repeat("开头", 30) // 60 rune
	base = head + "…后续"
	if got := joinContinuation(base, head+"完全重写"); got != base {
		t.Errorf("重写检测: got %q want %q", got, base)
	}
}

func TestRunAutoContinueCompletes(t *testing.T) {
	first := strings.Repeat("这是一段很长的回答内容，", 80) // 以逗号结尾，像被截断
	res := &StreamResult{Raw: mkRespLine(first)}

	hops := 0
	send := func(prev *StreamResult, hop int) (*StreamResult, error) {
		hops++
		// 续写重复了最后一个分句，再补上收尾。
		add := "这是一段很长的回答内容，后面还有一点。"
		return &StreamResult{Raw: mkRespLine(add)}, nil
	}
	text, out := runAutoContinue(first, res, 3, false, send)
	if hops != 1 {
		t.Fatalf("hops=%d want 1", hops)
	}
	if !strings.HasSuffix(text, "后面还有一点。") {
		t.Errorf("拼接结果不对: ...%q", text[max(0, len(text)-40):])
	}
	if out.Truncated {
		t.Error("已经收尾，不该标 Truncated")
	}
	if out.Emitted != "" {
		t.Errorf("非流式 Emitted 应为空: %q", out.Emitted)
	}
}

func TestRunAutoContinueStreamingKeepsEmittedInSync(t *testing.T) {
	first := strings.Repeat("这是一段很长的回答内容，", 80)
	res := &StreamResult{Raw: mkRespLine(first), Emitted: first}

	send := func(prev *StreamResult, hop int) (*StreamResult, error) {
		add := "后面还有一点。"
		return &StreamResult{Raw: mkRespLine(add), Emitted: add}, nil
	}
	text, out := runAutoContinue(first, res, 3, true, send)
	if out.Emitted != text {
		t.Errorf("流式 Emitted 应与正文一致: emitted=%d text=%d", len(out.Emitted), len(text))
	}
	if !strings.HasSuffix(text, "后面还有一点。") || out.Truncated {
		t.Errorf("结果不对: truncated=%v tail=%q", out.Truncated, text[max(0, len(text)-20):])
	}
}

func TestRunAutoContinueStopsAtMaxHops(t *testing.T) {
	first := strings.Repeat("这是一段很长的回答内容，", 80)
	res := &StreamResult{Raw: mkRespLine(first)}

	hops := 0
	send := func(prev *StreamResult, hop int) (*StreamResult, error) {
		hops++
		add := "还在往下写，"
		return &StreamResult{Raw: mkRespLine(add)}, nil
	}
	text, out := runAutoContinue(first, res, 2, false, send)
	if hops != 2 {
		t.Fatalf("hops=%d want 2", hops)
	}
	if !out.Truncated {
		t.Error("跳数用尽仍未收尾，应标 Truncated")
	}
	if !strings.HasSuffix(text, "还在往下写，") {
		t.Errorf("末尾不对: %q", text[max(0, len(text)-20):])
	}
}

func TestRunAutoContinueSkipsShortAnswer(t *testing.T) {
	res := &StreamResult{Raw: mkRespLine("好的")}
	send := func(prev *StreamResult, hop int) (*StreamResult, error) {
		t.Fatal("短回答不该触发续写")
		return nil, nil
	}
	text, out := runAutoContinue("好的", res, 3, false, send)
	if text != "好的" || out.Truncated {
		t.Errorf("短回答被误判: text=%q truncated=%v", text, out.Truncated)
	}
}

func TestIsNoOpContinuation(t *testing.T) {
	if !isNoOpContinuation("Already reached 2500. There is no remaining text.") {
		t.Error("英文空续写没识别出来")
	}
	if !isNoOpContinuation("已经写完，没有剩余内容。") {
		t.Error("中文空续写没识别出来")
	}
	// 长正文里恰好出现关键词，不能误判。
	if isNoOpContinuation(strings.Repeat("这段正文里恰好提到 already 这个词，", 20)) {
		t.Error("长正文被误判为空续写")
	}
}

func TestRunAutoContinueDropsNoOp(t *testing.T) {
	first := strings.Repeat("这是一段很长的回答内容，", 80)
	res := &StreamResult{Raw: mkRespLine(first)}
	send := func(prev *StreamResult, hop int) (*StreamResult, error) {
		return &StreamResult{Raw: mkRespLine("Already reached 2500. There is no remaining text.")}, nil
	}
	text, out := runAutoContinue(first, res, 2, false, send)
	if text != first {
		t.Errorf("空续写不该拼进正文: %q", text[max(0, len(text)-40):])
	}
	if out.Truncated {
		t.Error("上游表示已写完时不该标 Truncated")
	}
}
