package handlers

import (
	"strings"
	"testing"
)

func TestInjectSaveNamespace(t *testing.T) {
	tag := `<script src="/assets/savens-ns.js" data-ns="h5g76"></script>`

	cases := []struct {
		name     string
		html     string
		wantIdx  string // 期望出现在标签之后的锚点
		wantHead bool   // 标签是否位于该锚点之后
	}{
		{
			name:     "有 head",
			html:     "<!DOCTYPE html><html><head><meta charset=\"utf-8\"></head><body></body></html>",
			wantIdx:  "<meta charset",
			wantHead: true,
		},
		{
			name:     "带属性的 head",
			html:     "<html><head lang=\"zh\"><title>x</title></head><body></body></html>",
			wantIdx:  "<title>",
			wantHead: true,
		},
		{
			name:     "无 head 有 html",
			html:     "<html><body><script>1</script></body></html>",
			wantIdx:  "<body>",
			wantHead: true,
		},
		{
			name:     "裸 HTML（无 head/html）",
			html:     "<script>game()</script>",
			wantIdx:  "<script>game()</script>",
			wantHead: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := injectSaveNamespace(tc.html, "h5g76")
			if !strings.Contains(out, tag) {
				t.Fatalf("未注入标签: %s", out)
			}
			tagPos := strings.Index(out, tag)
			anchorPos := strings.Index(out, tc.wantIdx)
			if anchorPos < 0 {
				t.Fatalf("锚点缺失: %s", tc.wantIdx)
			}
			if tc.wantHead && tagPos > anchorPos {
				t.Fatalf("标签未插到锚点之前: tag=%d anchor=%d\n%s", tagPos, anchorPos, out)
			}
		})
	}
}

func TestInjectSaveNamespaceEmptyNS(t *testing.T) {
	html := "<html><head></head><body></body></html>"
	if got := injectSaveNamespace(html, ""); got != html {
		t.Fatalf("空 ns 不应改写 HTML，got: %s", got)
	}
}

func TestSaveNamespaceScriptSelfCheck(t *testing.T) {
	if saveNamespaceScript == "" {
		t.Fatal("脚本为空")
	}
	if strings.Contains(saveNamespaceScript, "`") {
		t.Fatal("脚本含反引号，无法用 Go 原样字符串承载")
	}
	for _, marker := range []string{"Storage", "IDBFactory", "__uogameNS", "data-ns", "getItem"} {
		if !strings.Contains(saveNamespaceScript, marker) {
			t.Fatalf("脚本缺少关键片段: %s", marker)
		}
	}
}