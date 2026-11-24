package app

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// 自动续写（#84）。
//
// 上游网页端把输入和输出放在同一个上下文窗口里算。对话越长、预填占得越多，留给输出的
// 空间越小，于是回答经常在写到一半时直接停住——不是报错，是正常结束，协议里也没有
// finishReason 可看。能做的只有：看出来它没写完，就在同一个会话里补一句「继续」，把
// 剩下的内容接上。
//
// 只对单轮、无 tools、无图片的请求启用：带 tools 时中途续写会把 ```tool_call``` 围栏
// 拦腰拼断；带图/媒体时续接的目标不是文本。多轮（multi_turn）路径复用 conv 状态，
// 匿名也能续；普通路径只有登录号能续——匿名会话的 cookie 在首轮之后没有留下来。
const (
	// continuationInstruction 要说得足够死，否则模型容易礼貌地重写整段。
	continuationInstruction = "Continue from exactly where you left off. Do not repeat anything you already wrote — output only the remaining text."

	// minAutoContinueLen 是触发续写的最短字符数（rune）。截断只发生在长回答上；
	// 短回答（"好的"之类）本来就没收尾标点是常态，不该为它多发一次请求。
	minAutoContinueLen = 600
)

var errAutoContinueNoConv = errors.New("auto-continue: response has no conversation id")

// looksIncomplete 判断一段回答是否像被截在半句。
//
// 只能靠启发式：上游不说自己完没完。规则按「中文优先、宁少勿滥」定：
//   - 太短的一律不判（见 minAutoContinueLen）；
//   - 奇数个 ``` / ` 说明代码块或行内代码没闭合；
//   - 以句末标点收尾的算写完，其余算没写完。
func looksIncomplete(s string) bool {
	t := strings.TrimRight(s, " \t\r\n")
	if utf8.RuneCountInString(t) < minAutoContinueLen {
		return false
	}
	if strings.Count(t, "```")%2 == 1 {
		return true
	}
	if strings.HasSuffix(t, "```") {
		return false
	}
	if strings.Count(t, "`")%2 == 1 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(t)
	switch r {
	case '。', '！', '？', '.', '!', '?', '…', '；', ';',
		')', '）', ']', '】', '}', '》', '」', '』',
		'"', '”', '’', '\'':
		return false
	}
	return true
}

// joinContinuation 把续写得到的新段接到已有正文后面。
//
// 续写最常见的开头是原样重复上一段的最后一句，所以先找最长重叠、只保留新增部分。
// 如果模型干脆从头重写（新段以已有正文的开头开始），就不再拼接——已发出的内容收不回，
// 再拼一遍等于把整段答案翻倍。
func joinContinuation(base, add string) string {
	add = strings.TrimLeft(add, " \t\r\n")
	if add == "" {
		return base
	}
	if head := firstRunes(base, 60); utf8.RuneCountInString(head) >= 40 && strings.HasPrefix(add, head) {
		return base
	}
	max := len(add)
	if max > len(base) {
		max = len(base)
	}
	if max > 4000 {
		max = 4000
	}
	for k := max; k > 0; k-- {
		if strings.HasSuffix(base, add[:k]) {
			add = add[k:]
			break
		}
	}
	if strings.TrimSpace(add) == "" {
		return base
	}
	// 半句被截断时直接拼；但如果上一段已经以句末标点收尾，补个换行免得两段粘在一起。
	if r, _ := utf8.DecodeLastRuneInString(base); strings.ContainsRune("。！？.!?…", r) &&
		!strings.HasSuffix(base, "\n") && !strings.HasPrefix(add, "\n") {
		add = "\n" + add
	}
	return base + add
}

func firstRunes(s string, n int) string {
	i := 0
	for pos := range s {
		if i == n {
			return s[:pos]
		}
		i++
	}
	return s
}

// runAutoContinue 在 text 看起来没写完时，反复调用 send 取下一段并拼接。
//
// send(prev, hop) 负责按 prev 里的会话 id 发一次续写请求，返回这一跳的结果；
// 返回错误或空内容就停止，把已经拿到的部分原样交回。最终 res.Truncated 表示
// 停止时是否仍未收尾——调用方据此回 finish_reason=length。
//
// streaming 为真时不做去重：增量已经推给客户端了，再拿去重后的文本当最终结果，
// Emitted 和正文就会对不上（客户端看到重复段，响应体里却没有）。这时以实际发出的
// Emitted 为准；非流式才做前缀去重，拿一份干净的文本。
func runAutoContinue(text string, res *StreamResult, maxHops int, streaming bool,
	send func(prev *StreamResult, hop int) (*StreamResult, error)) (string, *StreamResult) {
	if res == nil || maxHops <= 0 || !looksIncomplete(text) {
		return text, res
	}
	out := *res
	emitted, emittedReasoning, reasoning := res.Emitted, res.EmittedReasoning, res.Reasoning
	cur := res
	noOp := false
	for hop := 0; hop < maxHops && looksIncomplete(text); hop++ {
		r2, err := send(cur, hop)
		if err != nil || r2 == nil {
			logf("[auto-continue] hop %d 停止: %v", hop+1, err)
			break
		}
		add := extractResponseText(r2.Raw)
		if add == "" {
			logf("[auto-continue] hop %d 没有新内容，停止", hop+1)
			break
		}
		if isNoOpContinuation(add) {
			// 模型认为已经写完（常见于「数到 N 为止」这类本来就该停的回答）。
			// 这句是元回复，不是正文，丢掉；并且它反过来证明答案已完整。
			logf("[auto-continue] hop %d 上游表示已写完，丢弃: %q", hop+1, truncate(add, 80))
			noOp = true
			break
		}
		joined := text + strings.TrimLeft(add, " \t\r\n")
		if !streaming {
			joined = joinContinuation(text, add)
		}
		if joined == text {
			logf("[auto-continue] hop %d 没有可拼接的新内容，停止", hop+1)
			break
		}
		text = joined
		emitted += r2.Emitted
		emittedReasoning += r2.EmittedReasoning
		if r2.Reasoning != "" {
			reasoning += r2.Reasoning
		}
		out.TotalMs += r2.TotalMs
		if r2.UpstreamModel != "" {
			out.UpstreamModel = r2.UpstreamModel
		}
		cur = r2
		logf("[auto-continue] hop %d 补了 %d 字节", hop+1, len(add))
	}
	if streaming {
		// 以实际推给客户端的字节为准，避免去重/补换行让正文和 Emitted 分叉。
		text = emitted
	}
	out.Emitted = emitted
	out.EmittedReasoning = emittedReasoning
	out.Reasoning = reasoning
	out.Raw = cur.Raw
	out.ConvCID, out.ConvRID, out.ConvRCID, out.ConvTok26 =
		cur.ConvCID, cur.ConvRID, cur.ConvRCID, cur.ConvTok26
	out.Truncated = !noOp && looksIncomplete(text)
	return text, &out
}

// isNoOpContinuation 识别「已经写完了，没有剩余内容」这类元回复。
//
// 续写请求有时会碰上模型认为答案已经完整（请求里要求写到某个数量为止时尤其常见）。
// 它回的是「Already reached 2500. There is no remaining text.」这种话，不是正文，
// 拼上去只会污染答案。只在短回复里认，避免误伤正文中恰好出现这些词的长段落。
func isNoOpContinuation(s string) bool {
	t := strings.ToLower(strings.TrimSpace(s))
	if utf8.RuneCountInString(t) > 200 {
		return false
	}
	for _, p := range []string{
		"already reached", "already complete", "no remaining", "nothing left",
		"no more content", "nothing more", "that's all", "no additional",
		"已经写完", "已经完整", "没有剩余", "没有更多", "无需继续", "已至此",
	} {
		if strings.Contains(t, p) {
			return true
		}
	}
	return false
}
