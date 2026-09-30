package config

import (
	"sort"

	"github.com/zeromicro/go-zero/core/logx"
)

// 热更配置的单一真源。
//
// 背景：ApplyHotUpdate 的白名单判断与 restartRequiredKeysIn 的「须重启」探测，
// 描述的是同一份知识——「哪些配置键能热更、哪些必须重启」。此前两者各写一遍
// （一个 case 列表 + 一段逐层类型断言的 yaml 遍历，合计 90 余行），
// 新增一个配置字段要记得同步改三处（结构体、白名单、重启清单），漏一处就是静默不一致。
// 现在两侧都从 hotFields 派生。

// hotPolicy 单个配置键的热更策略。
type hotPolicy int

const (
	policyApply  hotPolicy = iota // 可立即生效
	policyReject                  // 须重启：不改内存，仅告警
)

// hotField 一个配置键的热更元数据。
type hotField struct {
	Key     string   // 规范点分键，用于日志与 restartRequiredKeysIn 的返回值
	Path    []string // yaml 路径，用于从 etcd 载荷中探测该键是否出现
	Aliases []string // 兼容旧名 / 大小写变体
	Policy  hotPolicy
}

const keyLogLevel = "Log.Level"

// hotFields 全部受管配置键。未列出的键一律按「不在白名单」忽略。
var hotFields = []hotField{
	// 唯一可热更项：改级别不动连接，重启代价 ≫ 收益
	{Key: keyLogLevel, Path: []string{"Log", "Level"}, Aliases: []string{"log.level"}, Policy: policyApply},

	// 以下均须重启：涉及连接串/端口/监听地址/鉴权模式，热改会让内存与真实资源不一致
	{Key: "Timeout", Path: []string{"Timeout"}, Aliases: []string{"timeout"}, Policy: policyReject},
	{Key: "Port", Path: []string{"Port"}, Policy: policyReject},
	{Key: "Host", Path: []string{"Host"}, Policy: policyReject},

	{Key: "DB.DataSource", Path: []string{"DB", "DataSource"}, Policy: policyReject},
	{Key: "DB.DSN", Path: []string{"DB", "DSN"}, Policy: policyReject},

	{Key: "RedisConf.Host", Path: []string{"RedisConf", "Host"}, Policy: policyReject},
	{Key: "RedisConf.User", Path: []string{"RedisConf", "User"}, Policy: policyReject},
	{Key: "RedisConf.Pass", Path: []string{"RedisConf", "Pass"}, Policy: policyReject},
	{Key: "Redis.Host", Path: []string{"Redis", "Host"}, Policy: policyReject}, // 旧名兼容
	{Key: "Redis.Pass", Path: []string{"Redis", "Pass"}, Policy: policyReject},

	{Key: "OSS.Endpoint", Path: []string{"OSS", "Endpoint"}, Policy: policyReject},
	{Key: "OSS.Bucket", Path: []string{"OSS", "Bucket"}, Policy: policyReject},
	{Key: "OSS.AccessKeyID", Path: []string{"OSS", "AccessKeyID"}, Policy: policyReject},
	{Key: "OSS.AccessKeySecret", Path: []string{"OSS", "AccessKeySecret"}, Policy: policyReject},

	{Key: "MQ.Endpoint", Path: []string{"MQ", "Endpoint"}, Policy: policyReject},
	{Key: "MQ.InstanceID", Path: []string{"MQ", "InstanceID"}, Policy: policyReject},
	{Key: "MQ.Topic", Path: []string{"MQ", "Topic"}, Policy: policyReject},
	{Key: "MQ.Group", Path: []string{"MQ", "Group"}, Policy: policyReject},
	{Key: "MQ.AccessKey", Path: []string{"MQ", "AccessKey"}, Policy: policyReject},
	{Key: "MQ.AccessKeySecret", Path: []string{"MQ", "AccessKeySecret"}, Policy: policyReject},

	{Key: "SLS.Endpoint", Path: []string{"SLS", "Endpoint"}, Policy: policyReject},
	{Key: "SLS.Project", Path: []string{"SLS", "Project"}, Policy: policyReject},
	{Key: "SLS.Logstore", Path: []string{"SLS", "Logstore"}, Policy: policyReject},
	{Key: "SLS.AccessKeyID", Path: []string{"SLS", "AccessKeyID"}, Policy: policyReject},
	{Key: "SLS.AccessKeySecret", Path: []string{"SLS", "AccessKeySecret"}, Policy: policyReject},

	{Key: "Etcd.Hosts", Path: []string{"Etcd", "Hosts"}, Policy: policyReject},
	{Key: "Auth.Mode", Path: []string{"Auth", "Mode"}, Policy: policyReject},
	{Key: "RpcServer.ListenOn", Path: []string{"RpcServer", "ListenOn"}, Policy: policyReject},
	{Key: "RpcClient.Targets", Path: []string{"RpcClient", "Targets"}, Policy: policyReject},
	{Key: "RpcClient.BlockDial", Path: []string{"RpcClient", "BlockDial"}, Policy: policyReject},
}

// hotFieldIndex 规范键 + 别名 → hotField；构建期一次性展开，避免每次热更重复遍历。
var hotFieldIndex = func() map[string]hotField {
	m := make(map[string]hotField, len(hotFields)*2)
	for _, f := range hotFields {
		m[f.Key] = f
		for _, alias := range f.Aliases {
			m[alias] = f
		}
	}
	return m
}()

func lookupHotField(key string) (hotField, bool) {
	f, ok := hotFieldIndex[key]
	return f, ok
}

// yamlHasPath 判断 etcd 载荷的 yaml 树里是否出现了给定路径的叶子键。
// 只认 map 逐层下钻；中途类型不符即视为不存在（与旧实现的类型断言行为一致）。
func yamlHasPath(root map[string]any, path []string) bool {
	cur := root
	for i, seg := range path {
		v, ok := cur[seg]
		if !ok {
			return false
		}
		if i == len(path)-1 {
			return true
		}
		next, ok := v.(map[string]any)
		if !ok {
			return false
		}
		cur = next
	}
	return false
}

// ApplyHotUpdate 应用热更载荷：命中白名单才改内存，其余只告警。
// 自行加 globalMu 写锁（可被 etcd watch goroutine 与测试并发调用）。
func ApplyHotUpdate(patch map[string]any) {
	globalMu.Lock()
	defer globalMu.Unlock()

	for k, v := range patch {
		f, ok := lookupHotField(k)
		if !ok {
			logWarnf("hot update ignored (not in whitelist): %s", k)
			continue
		}
		if f.Policy == policyReject {
			logWarnf("hot update rejected (requires restart): %s", f.Key)
			continue
		}
		// 新增「可热更」键时必须在此补 case：hotFields 只声明策略与路径，
		// 具体怎么写回内存仍由这里决定，漏补会静默什么都不做。
		switch f.Key {
		case keyLogLevel:
			applyLogLevel(v)
		default:
			logWarnf("hot update has no apply handler, ignored: %s", f.Key)
		}
	}
}

// applyLogLevel 解析并应用 Log.Level；非法值只告警、不改内存。
func applyLogLevel(v any) {
	s, ok := v.(string)
	if !ok {
		logWarnf("hot update rejected (Log.Level must be string, got %T)", v)
		return
	}
	lvl, ok := parseLevel(s)
	if !ok {
		logWarnf("hot update rejected (invalid Log.Level %q)", s)
		return
	}
	// 规范化：warn/warning → info；severe → error（与 logx 级别集合对齐）
	s = normalizeLogLevelName(s)
	global.Log.Level = s
	logx.SetLevel(lvl)
	logx.Infof("hot update applied: Log.Level=%s", s)
}

// restartRequiredKeysIn 从 etcd 载荷中挑出「已出现但须重启」的配置键（升序）。
// 与 ApplyHotUpdate 共用 hotFields，因此不会出现「白名单认了、重启清单不认」的漂移。
func restartRequiredKeysIn(norm map[string]any) []string {
	var keys []string
	for _, f := range hotFields {
		if f.Policy != policyReject {
			continue
		}
		if yamlHasPath(norm, f.Path) {
			keys = append(keys, f.Key)
		}
	}
	sort.Strings(keys)
	return keys
}
