package app

import "testing"

func TestCleanCitations(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		final bool
		want  string
	}{
		{"末尾单个", "答案。[cite: 1]", true, "答案。"},
		{"无空格写法", "答案。[cite:1]", true, "答案。"},
		{"连续多个", "结论成立。[cite: 1][cite: 2, 3]", true, "结论成立。"},
		{"区间写法", "按官方文档。[cite: 1-3]", true, "按官方文档。"},
		{"夹在中间", "前文 [cite: 1] 后文", true, "前文  后文"},
		{"没有标记", "这是一段普通回答。[1] 不是引用标记。", true, "这是一段普通回答。[1] 不是引用标记。"},
		{"协议字段不误伤", "header 下标 14 / inner[79] 才是模型开关。", true, "header 下标 14 / inner[79] 才是模型开关。"},
		{"流式半截扣住", "答案 [cite", false, "答案 "},
		{"流式半截带冒号", "答案 [cite:", false, "答案 "},
		{"流式整段标记", "答案 [cite: 1]", false, "答案 "},
		{"流式普通文本", "答案正", false, "答案正"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cleanCitations(c.in, c.final); got != c.want {
				t.Fatalf("cleanCitations(%q, %v) = %q, want %q", c.in, c.final, got, c.want)
			}
		})
	}
}
