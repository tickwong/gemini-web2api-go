package app

import (
	"regexp"
	"strings"
)

// 网页端答案里的引用标记，形如 [cite: 1]、[cite:1]、[cite: 1, 2]。
//
// 正常在 gemini.google.com 上，前端会把这些标记渲染成可点的来源角标，真正的来源 URL
// 放在响应元数据的 grounding chunk 里（见 search.go 的 extractGrounding）。走反代时
// 正文原样返回、元数据没人消费，标记就成了正文里一串看不懂的裸文本，所以在这里清掉。
//
// 只认带 cite: 的方括号，别写宽成 `\[\d+\]`——那会误伤 Markdown 里的普通引用编号。
var citeMarkerRe = regexp.MustCompile(`(?i)\[cite:\s*\d+(?:\s*[,\-–—]\s*\d+)*\s*\]`)

// 标记的前缀：流式分片可能停在 "[c"、"[cite"、"[cite:" 任意一处，都要按前缀扣住。
const citePrefix = "[cite:"

// citePrefixRe 匹配「前半截合法、还没闭合」的引用标记，如 `[cite: 1`。
var citePrefixRe = regexp.MustCompile(`(?i)^\[cite:[\s\d,\-–—]*$`)

// cleanCitations 去掉正文里的引用标记。
//
// final=false（流式分片）时，末尾若正停在标记的半截上（`[c`、`[cite`、`[cite: 1` 等），
// 先扣住不下发，等后面的分片补全标记、被整段删掉，避免客户端先看到「[cite」再看到它凭空消失。
func cleanCitations(text string, final bool) string {
	if text == "" {
		return text
	}
	text = citeMarkerRe.ReplaceAllString(text, "")
	if final {
		return text
	}
	lower := strings.ToLower(text)
	i := strings.LastIndex(lower, "[")
	if i < 0 {
		return text
	}
	tail := lower[i:]
	if strings.HasPrefix(citePrefix, tail) || citePrefixRe.MatchString(tail) {
		return text[:i]
	}
	return text
}
