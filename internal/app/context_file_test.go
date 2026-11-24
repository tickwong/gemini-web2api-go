package app

import "testing"

// 附件在 inner[0] 第 4 槽里的形状：[[路径, 类型, null, mime], 文件名, null×6, [0]]。
// 类型 1=图片 / 3=文本；Kind 未填时按文本，Mime 未填时按 text/plain。
func TestFileAttachmentRefs(t *testing.T) {
	if got := fileAttachmentRefs(nil); got != nil {
		t.Fatalf("空附件应返回 nil，got %v", got)
	}

	refs := fileAttachmentRefs([]fileRef{{Ref: "viking://ref/1", Name: "message.txt"}})
	if len(refs) != 1 {
		t.Fatalf("refs 数量 = %d", len(refs))
	}
	row, ok := refs[0].([]interface{})
	if !ok || len(row) != 9 {
		t.Fatalf("单条附件应有 9 槽，got %#v", refs[0])
	}
	head, ok := row[0].([]interface{})
	if !ok || len(head) != 4 {
		t.Fatalf("路径头应有 4 槽，got %#v", row[0])
	}
	if head[0] != "viking://ref/1" || head[1] != 3 || head[2] != nil || head[3] != "text/plain" {
		t.Errorf("路径头 = %#v", head)
	}
	if row[1] != "message.txt" {
		t.Errorf("文件名 = %#v", row[1])
	}
	tail, ok := row[8].([]interface{})
	if !ok || len(tail) != 1 || tail[0] != 0 {
		t.Errorf("末槽 = %#v", row[8])
	}
}
