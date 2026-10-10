package handlers

import (
	"testing"

	"uogame-go/internal/database"
	"uogame-go/internal/models"
)

// TestLibretroNameCandidates 校验候选名称生成：原名 / 去括号后缀 / 追加区域后缀 / 去重
func TestLibretroNameCandidates(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantHead []string // 前缀部分（顺序固定）
		wantNil  bool
	}{
		{"空串返回 nil", "", nil, true},
		{"纯空白返回 nil", "   ", nil, true},
		{"无括号仅原名", "Super Mario", []string{"Super Mario"}, false},
		{"圆括号版本后缀", "Super Mario (USA)", []string{"Super Mario (USA)", "Super Mario"}, false},
		{"方括号区域后缀", "Contra [U]", []string{"Contra [U]", "Contra"}, false},
		{"首字符即括号时不裁剪主名", "(Hack)", []string{"(Hack)"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := libretroNameCandidates(tc.in)
			if tc.wantNil {
				if got != nil {
					t.Fatalf("期望 nil，实际 %v", got)
				}
				return
			}
			// 全部候选中必须包含主名的常见区域后缀变体
			stem := tc.wantHead[len(tc.wantHead)-1]
			if !containsString(got, stem+" (USA)") || !containsString(got, stem+" (Japan, USA)") {
				t.Fatalf("缺少区域后缀候选，实际 %v", got)
			}
			// 候选总数 = 主名前缀数 + 区域数 - 去重抵消项
			if len(got) < len(tc.wantHead) || len(got) > len(tc.wantHead)+len(libretroRegions) {
				t.Fatalf("候选数量异常 got=%d(%v)", len(got), got)
			}
			for i := range tc.wantHead {
				if got[i] != tc.wantHead[i] {
					t.Fatalf("第 %d 项不符 got=%q want=%q", i, got[i], tc.wantHead[i])
				}
			}
		})
	}
}

// containsString 判断字符串切片是否包含目标值
func containsString(list []string, target string) bool {
	for _, s := range list {
		if s == target {
			return true
		}
	}
	return false
}

// TestLibretroNameCandidatesRegionSuffix 校验区域后缀候选确实生成（覆盖 libretro 命名习惯）
func TestLibretroNameCandidatesRegionSuffix(t *testing.T) {
	got := libretroNameCandidates("1942")
	if got[0] != "1942" || got[1] != "1942 (USA)" {
		t.Fatalf("前两项应为原名与高概率区域后缀，实际 %v", got[:2])
	}
	for _, want := range []string{"1942 (Japan, USA)", "1942 (Europe)", "1942 (Japan)"} {
		if !containsString(got, want) {
			t.Errorf("候选 %q 缺失，实际候选=%v", want, got)
		}
	}
}

// TestLibretroNormalize 校验归一化：去 .png、去括号标签、小写、非字母数字折算空格
func TestLibretroNormalize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1942 (Japan, USA).png", "1942"},
		{"Tekken (USA).png", "tekken"},
		{"Super Mario Bros. 3 (USA).png", "super mario bros 3"},
		{"Contra [U][!].png", "contra"},
		{"2-in-1 SuperSpike V'Ball + World Cup Soccer (1990-12)(Nintendo)(US).png",
			"2 in 1 superspike v ball world cup soccer"},
		{"1942", "1942"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := libretroNormalize(tc.in); got != tc.want {
			t.Errorf("归一化 %q：got=%q want=%q", tc.in, got, tc.want)
		}
	}
}

// TestLibretroPickBest 校验索引模糊匹配：精确命中、区域后缀命中、避免误匹配
func TestLibretroPickBest(t *testing.T) {
	index := []string{
		"1942%20(Japan%2C%20USA).png",
		"1942%20(Japan%2C%20USA)%5bb%5d.png",
		"Tekken%20(USA).png",
		"Contra%20Force%20(USA).png",
		"Super%20Mario%20Bros.%203%20(USA).png",
		"Tetris%202%20(USA).png",
	}
	cases := []struct {
		name  string
		query string
		want  string
	}{
		{"简名命中（优选无方括号标签项）", "1942", "1942%20(Japan%2C%20USA).png"},
		{"区域后缀命中", "Tekken", "Tekken%20(USA).png"},
		{"点号差异归一化命中", "Super Mario Bros 3", "Super%20Mario%20Bros.%203%20(USA).png"},
		{"不应把 Contra 误配到 Contra Force", "Contra", ""},
		{"不应把 Tetris 误配到 Tetris 2", "Tetris", ""},
		{"完全无匹配", "Zelda", ""},
		{"空查询", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := libretroPickBest(tc.query, index); got != tc.want {
				t.Fatalf("got=%q want=%q", got, tc.want)
			}
		})
	}
}

// TestLibretroPickBestPrefersTypical 校验同名词条择优：
// 优先「干净区域版本」而非 PlayChoice-10 / No-Intro 带发行商日期 / [b] 等冷门版本
func TestLibretroPickBestPrefersTypical(t *testing.T) {
	// 场景一：存在干净的 (USA) 版本 → 必须选它
	withPlain := []string{
		"Contra%20(1988)(Konami)(PlayChoice-10).png",
		"Contra%20(1988-02)(Konami)(US).png",
		"Contra%20(Japan).png",
		"Contra%20(USA).png",
		"Contra%20(USA)%5bb%5d.png",
	}
	if got := libretroPickBest("Contra", withPlain); got != "Contra%20(USA).png" {
		t.Errorf("存在干净 (USA) 版本时应选它，实际 %q", got)
	}

	// 场景二：只有 No-Intro 命名 → 应选带区域标记的正式版，而非 PlayChoice-10 街机版
	noIntroOnly := []string{
		"Contra%20(1988)(Konami)(PlayChoice-10).png",
		"Contra%20(1988-02)(Konami)(US).png",
	}
	if got := libretroPickBest("Contra", noIntroOnly); got != "Contra%20(1988-02)(Konami)(US).png" {
		t.Errorf("应按典型度选中 (US) 正式版，实际 %q", got)
	}

	// 场景三：多区域组合分组（Japan, USA）应被视为区域标记而非修饰
	multi := []string{
		"1942%20(1988)(Capcom)(PlayChoice-10).png",
		"1942%20(Japan%2C%20USA).png",
	}
	if got := libretroPickBest("1942", multi); got != "1942%20(Japan%2C%20USA).png" {
		t.Errorf("多区域组合应被识别为区域标记，实际 %q", got)
	}
}

// TestJaroWinkler 校验相似度基本性质
func TestJaroWinkler(t *testing.T) {
	if got := jaroWinkler("contra", "contra"); got != 1 {
		t.Fatalf("完全相同应为 1，实际 %v", got)
	}
	if got := jaroWinkler("contra", "contra force"); got >= libretroMatchThreshold {
		t.Fatalf("contra/contra force 相似度应低于阈值，实际 %v", got)
	}
	if got := jaroWinkler("", "x"); got != 0 {
		t.Fatalf("空串应为 0，实际 %v", got)
	}
	if got := jaroWinkler("tekken", "tekken"); got != 1 {
		t.Fatalf("完全相同应为 1，实际 %v", got)
	}
}

// TestSettingInt 校验整型设置读取：缺失 / 非法 / 越界均回退默认值
func TestSettingInt(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	if got := settingInt(settingScrapeTimeout, 60, 5, 300); got != 60 {
		t.Fatalf("缺失时应回退默认 60，实际 %d", got)
	}
	database.DB.Create(&models.Setting{Key: settingScrapeTimeout, Value: "abc"})
	if got := settingInt(settingScrapeTimeout, 60, 5, 300); got != 60 {
		t.Fatalf("非法值应回退默认 60，实际 %d", got)
	}
	database.DB.Save(&models.Setting{Key: settingScrapeTimeout, Value: "999"})
	if got := settingInt(settingScrapeTimeout, 60, 5, 300); got != 60 {
		t.Fatalf("越界值应回退默认 60，实际 %d", got)
	}
	database.DB.Save(&models.Setting{Key: settingScrapeTimeout, Value: "120"})
	if got := settingInt(settingScrapeTimeout, 60, 5, 300); got != 120 {
		t.Fatalf("合法值应取 120，实际 %d", got)
	}
}

// TestPlatformTagCodeFromLabel 校验中文标签 → 平台码反查
func TestPlatformTagCodeFromLabel(t *testing.T) {
	cases := []struct {
		label string
		want  string
	}{
		{"任天堂 GBA", "gba"},
		{"红白机 NES", "nes"},
		{"HTML5", "h5"},
		{"不存在的标签", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := platformTagCodeFromLabel(tc.label); got != tc.want {
			t.Errorf("标签 %q：got=%q want=%q", tc.label, got, tc.want)
		}
	}
}

// TestDedupeStrings 校验去重且保持顺序、过滤空串
func TestDedupeStrings(t *testing.T) {
	got := dedupeStrings([]string{"a", "b", "a", "", "c", "b"})
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("长度不符 got=%v want=%v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("第 %d 项不符 got=%q want=%q", i, got[i], want[i])
		}
	}
}

// TestMimeFromURL 校验从 URL 扩展名推断 MIME
func TestMimeFromURL(t *testing.T) {
	cases := []struct {
		u    string
		want string
	}{
		{"https://x/a.png", "image/png"},
		{"https://x/a.JPG", "image/jpeg"},
		{"https://x/a.jpeg", "image/jpeg"},
		{"https://x/a.webp", "image/webp"},
		{"https://x/a.gif", "image/gif"},
		{"https://x/a", "image/png"},
	}
	for _, tc := range cases {
		if got := mimeFromURL(tc.u); got != tc.want {
			t.Errorf("URL %q：got=%q want=%q", tc.u, got, tc.want)
		}
	}
}

// TestScrapeGameMissed 无匹配 provider 时应返回 Missed=true
func TestScrapeGameMissed(t *testing.T) {
	orig := metadataProviders
	defer func() { metadataProviders = orig }()
	metadataProviders = []MetadataProvider{fakeProvider{}}

	res := scrapeGame(&models.Game{Name: "X", PlatformTag: "HTML5"})
	if !res.Missed {
		t.Fatalf("期望 Missed=true，实际 %+v", res)
	}
}

// fakeProvider 永远不命中的假 provider，用于编排回退测试
type fakeProvider struct{}

func (fakeProvider) Name() string                          { return "fake" }
func (fakeProvider) Enabled() bool                          { return true }
func (fakeProvider) Search(_, _ string) (*ScrapedMetadata, error) { return nil, nil }
