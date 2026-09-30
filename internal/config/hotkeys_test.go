package config

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/logx"
)

// legacyHotKeys 仅用于识别历史配置名：Config 上本就没有同名字段，
// 登记它们是为了让旧 yaml 里的键被判「须重启」而不是「不在白名单」。
// 新增条目必须先确认该字段真的不该存在于 Config 上。
var legacyHotKeys = map[string]bool{
	"Redis.Host": true,
	"Redis.Pass": true,
	"DB.DSN":     true,
}

// 契约：hotFields 声明的 yaml 路径必须真能落到 Config 的字段上。
//
// 这条用例来自一次真实事故：RpcClient.Targets 先被登记进热更注册表、
// 结构体却还没有 RpcClient 字段，于是一份含 RpcClient 的 etcd 载荷
// 只会打到「不在白名单」——静默失效，编译期与运行期都没有任何提示。
func TestHotFields_PathsExistOnConfigStruct(t *testing.T) {
	typ := reflect.TypeOf(Config{})
	for _, f := range hotFields {
		if legacyHotKeys[f.Key] {
			continue
		}
		require.True(t, structHasPath(typ, f.Path),
			"%s（yaml 路径 %v）在 Config 上找不到对应字段；请补字段或登记为 legacyHotKeys", f.Key, f.Path)
	}

	// legacy 名单不得成为万能挡箭牌：逐条确认它确实没有对应字段
	for key := range legacyHotKeys {
		require.False(t, structHasPath(typ, mustField(t, key).Path),
			"%s 已能落到 Config 字段上，应从 legacyHotKeys 移除", key)
	}
}

func structHasPath(t reflect.Type, path []string) bool {
	for _, seg := range path {
		for t.Kind() == reflect.Ptr || t.Kind() == reflect.Slice {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct {
			return false
		}
		next, ok := structFieldByName(t, seg)
		if !ok {
			return false
		}
		t = next
	}
	return true
}

// structFieldByName 按 go-zero mapping 的匹配规则（不分大小写）找字段。
func structFieldByName(t reflect.Type, name string) (reflect.Type, bool) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			if got, ok := structFieldByName(f.Type, name); ok {
				return got, true
			}
			continue
		}
		if strings.EqualFold(f.Name, name) {
			return f.Type, true
		}
	}
	return nil, false
}

func mustField(t *testing.T, key string) hotField {
	t.Helper()
	f, ok := lookupHotField(key)
	require.True(t, ok, "未登记 %s", key)
	return f
}

func countPolicy(p hotPolicy) int {
	n := 0
	for _, f := range hotFields {
		if f.Policy == p {
			n++
		}
	}
	return n
}

// 契约：白名单（ApplyHotUpdate）与「须重启」清单（restartRequiredKeysIn）共用 hotFields，
// 不存在只被一侧认识的键——这正是本次收敛掉的那份重复。
func TestHotFields_ConsistentRegistry(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range hotFields {
		require.NotEmpty(t, f.Key)
		require.NotEmpty(t, f.Path, "%s 缺 Path，restartRequiredKeysIn 将永远探测不到", f.Key)
		require.False(t, seen[f.Key], "%s 重复登记", f.Key)
		seen[f.Key] = true

		got, ok := lookupHotField(f.Key)
		require.True(t, ok, "%s 未进入索引", f.Key)
		require.Equal(t, f.Key, got.Key)

		for _, a := range f.Aliases {
			ag, ok := lookupHotField(a)
			require.True(t, ok, "别名 %s 未进入索引", a)
			require.Equal(t, f.Key, ag.Key, "别名 %s 指向了 %s 而非 %s", a, ag.Key, f.Key)
		}
	}

	require.Equal(t, policyApply, mustField(t, keyLogLevel).Policy, "Log.Level 须是可热更项")
	require.Equal(t, 1, countPolicy(policyApply),
		"可热更项只应有 Log.Level 一个；新增须同步 README 与 error-code/配置文档")
}

// 契约：被判「须重启」的键必须同时是白名单成员（策略 reject）。
// 否则 ApplyHotUpdate 会打「不在白名单」，与 restartRequiredKeysIn 的结论自相矛盾。
func TestRestartRequiredKeys_AreWhitelistedAsReject(t *testing.T) {
	norm := map[string]any{
		"Log":       map[string]any{"Level": "debug"},
		"Timeout":   100,
		"Port":      8080,
		"RpcServer": map[string]any{"ListenOn": "0.0.0.0:8081"},
		"DB":        map[string]any{"DataSource": "x"},
		"Redis":     map[string]any{"Pass": "p"}, // 旧名
	}
	keys := restartRequiredKeysIn(norm)
	require.NotEmpty(t, keys)
	for _, k := range keys {
		require.Equal(t, policyReject, mustField(t, k).Policy, "%s 被判须重启但策略不是 reject", k)
	}
	require.NotContains(t, keys, keyLogLevel, "Log.Level 可热更，不得出现在须重启清单")
	require.IsIncreasing(t, keys, "返回结果须有序")
}

func TestYamlHasPath(t *testing.T) {
	norm := map[string]any{
		"DB":  map[string]any{"DataSource": "x"},
		"Bad": "not-a-map",
	}
	require.True(t, yamlHasPath(norm, []string{"DB", "DataSource"}))
	require.True(t, yamlHasPath(norm, []string{"DB"}))
	require.False(t, yamlHasPath(norm, []string{"DB", "DSN"}))
	require.False(t, yamlHasPath(norm, []string{"DB", "DataSource", "deeper"}), "叶子之后不得继续下钻")
	require.False(t, yamlHasPath(norm, []string{"Bad", "X"}), "中途非 map 视为不存在")
	require.False(t, yamlHasPath(norm, []string{"Nope"}))
}

// 契约：须重启键即便出现在热更载荷里也不得改内存；不在白名单的键只告警。
func TestApplyHotUpdate_RejectsRestartRequired(t *testing.T) {
	logx.Disable()
	t.Setenv("APP_ENV", "dev")
	_, err := Load(filepath.Join("..", "..", "config"))
	require.NoError(t, err)

	before := Get()
	applyHotLogLevelRestore(t)

	ApplyHotUpdate(map[string]any{
		"Log.Level":     "debug",
		"DB.DataSource": "postgres://evil",
		"Timeout":       1,
		"Not.A.Key":     1, // 不在白名单 → 仅告警
	})

	after := Get()
	require.Equal(t, "debug", after.Log.Level, "白名单项须生效")
	require.Equal(t, before.DB.DataSource, after.DB.DataSource, "须重启键不得改内存")
	require.Equal(t, before.Timeout, after.Timeout, "须重启键不得改内存")
}

// applyHotLogLevelRestore 测试结束把 Log.Level 还原，避免影响同包其他用例。
func applyHotLogLevelRestore(t *testing.T) {
	t.Helper()
	orig := Get().Log.Level
	t.Cleanup(func() { ApplyHotUpdate(map[string]any{keyLogLevel: orig}) })
}
