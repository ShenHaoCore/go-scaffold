package config

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"strings"
	"time"

	"micro-scaffold/pkg/env"
	"micro-scaffold/pkg/secure"

	"github.com/zeromicro/go-zero/core/logx"
	"go.etcd.io/etcd/client/pkg/v3/transport"
	clientv3 "go.etcd.io/etcd/client/v3"
	"gopkg.in/yaml.v2"
)

// StartHotReload 启动 Etcd 配置热更；未配置 Etcd 时打 hot-reload disabled。
// 启用 Etcd 时一律须 ETCD_HOTRELOAD_TOKEN；prod 另须 TLS 或 ETCD_ALLOW_INSECURE=true。
func StartHotReload(ctx context.Context, etcd EtcdConf) error {
	if len(etcd.Hosts) == 0 || etcd.Key == "" {
		logx.Info("hot-reload disabled")
		return nil
	}
	if strings.TrimSpace(os.Getenv("ETCD_HOTRELOAD_TOKEN")) == "" {
		return fmt.Errorf("etcd: ETCD_HOTRELOAD_TOKEN is required when Etcd is configured")
	}
	tlsCfg, err := etcdClientTLS(etcd)
	if err != nil {
		return err
	}
	if tlsCfg == nil {
		if env.IsProd() && !etcdInsecureAllowed() {
			return fmt.Errorf("etcd hot-reload forbidden in prod without TLS: set Etcd CertFile/KeyFile/CAFile (or ETCD_*_FILE) or ETCD_ALLOW_INSECURE=true for break-glass")
		}
		msg := "SECURITY: etcd hot-reload enabled with plaintext client (no TLS). Prefer CertFile/KeyFile/CAFile; do not use plaintext in production."
		if env.IsProd() {
			logx.Severe(msg)
		} else {
			logx.Infof("[warn] %s", msg)
		}
	} else {
		logx.Infof("hot-reload using etcd TLS client")
	}
	go watchEtcd(ctx, etcd, tlsCfg)
	return nil
}

func etcdInsecureAllowed() bool {
	v := strings.TrimSpace(os.Getenv("ETCD_ALLOW_INSECURE"))
	return strings.EqualFold(v, "true") || v == "1"
}

func etcdClientTLS(etcd EtcdConf) (*tls.Config, error) {
	cert := strings.TrimSpace(etcd.CertFile)
	key := strings.TrimSpace(etcd.KeyFile)
	ca := strings.TrimSpace(etcd.CAFile)
	if cert == "" && key == "" && ca == "" {
		return nil, nil
	}
	if cert == "" || key == "" || ca == "" {
		return nil, fmt.Errorf("etcd TLS incomplete: need CertFile, KeyFile and CAFile together (or leave all empty for plaintext)")
	}
	tlsInfo := transport.TLSInfo{
		CertFile:      cert,
		KeyFile:       key,
		TrustedCAFile: ca,
	}
	cfg, err := tlsInfo.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("etcd TLS client config: %w", err)
	}
	return cfg, nil
}

func watchEtcd(ctx context.Context, etcd EtcdConf, tlsCfg *tls.Config) {
	const maxBackoff = 30 * time.Second
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		established, err := runEtcdWatchSession(ctx, etcd, tlsCfg)
		if ctx.Err() != nil {
			return
		}
		// 曾成功进入 Watch：断线后从 1s 重试；仅连续连不上时指数退避
		if established {
			backoff = time.Second
		}
		logx.Errorf("hot-reload watch session ended: %v; retry in %s", err, backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if !established && backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

func runEtcdWatchSession(ctx context.Context, etcd EtcdConf, tlsCfg *tls.Config) (established bool, err error) {
	cfg := clientv3.Config{
		Endpoints:   etcd.Hosts,
		DialTimeout: 5 * time.Second,
		TLS:         tlsCfg,
	}
	cli, err := clientv3.New(cfg)
	if err != nil {
		return false, err
	}
	defer func() { _ = cli.Close() }()

	logx.Infof("hot-reload watching etcd key=%s hosts=%v tls=%v", etcd.Key, etcd.Hosts, tlsCfg != nil)

	var rch clientv3.WatchChan
	if resp, err := cli.Get(ctx, etcd.Key); err == nil {
		for _, kv := range resp.Kvs {
			applyEtcdValue(kv.Value)
		}
		// 从 Get 之后的版本开始 Watch，避免 Get→Watch 建立之间的事件丢失
		rch = cli.Watch(ctx, etcd.Key, clientv3.WithRev(resp.Header.Revision+1))
		// 只有 Get 成功才算「曾建立连接」。此处曾是无条件赋值，导致
		// watchEtcd 每轮都把 backoff 重置为 1s，指数退避（maxBackoff）成死代码。
		established = true
	} else {
		if ctx.Err() == nil {
			logx.Errorf("hot-reload initial get failed: %v", err)
		}
		rch = cli.Watch(ctx, etcd.Key)
	}
	for wr := range rch {
		if wr.Err() != nil {
			return established, wr.Err()
		}
		established = true // 收到 watch 响应即视为已建立
		for _, ev := range wr.Events {
			if ev.Kv == nil {
				continue
			}
			applyEtcdValue(ev.Kv.Value)
		}
	}
	if ctx.Err() != nil {
		return established, ctx.Err()
	}
	return established, fmt.Errorf("etcd watch channel closed")
}

func applyEtcdValue(raw []byte) {
	const maxEtcdPayload = 256 << 10 // 256KiB
	if len(raw) > maxEtcdPayload {
		logx.Errorf("hot-reload: etcd payload too large (%d bytes), ignored", len(raw))
		return
	}
	var root any
	if err := yaml.Unmarshal(raw, &root); err != nil {
		logx.Errorf("hot-reload yaml unmarshal failed: %v", err)
		return
	}
	norm, ok := normalize(root).(map[string]any)
	if !ok {
		logx.Errorf("hot-reload: root must be a map")
		return
	}
	if err := verifyHotReloadToken(norm); err != nil {
		logx.Errorf("hot-reload: %v", err)
		return
	}
	patch := map[string]any{}
	if logMap, ok := norm["Log"].(map[string]any); ok {
		if lvl, ok := logMap["Level"].(string); ok {
			patch["Log.Level"] = lvl
		}
	}
	// 禁止热更键：仍传入 ApplyHotUpdate，打「须重启」Warn，不改内存
	for _, k := range restartRequiredKeysIn(norm) {
		patch[k] = true
	}
	if len(patch) == 0 {
		logx.Infof("hot-reload: no whitelisted fields in update")
		return
	}
	ApplyHotUpdate(patch)
}

// verifyHotReloadToken：若设置 ETCD_HOTRELOAD_TOKEN，载荷须含匹配的 HotReloadToken 字段。
func verifyHotReloadToken(norm map[string]any) error {
	want := strings.TrimSpace(os.Getenv("ETCD_HOTRELOAD_TOKEN"))
	if want == "" {
		return nil
	}
	got, _ := norm["HotReloadToken"].(string)
	got = strings.TrimSpace(got)
	if !secure.EqualString(got, want) {
		return fmt.Errorf("HotReloadToken mismatch or missing (ETCD_HOTRELOAD_TOKEN configured)")
	}
	return nil
}

// restartRequiredKeysIn 见 hotkeys.go（与 ApplyHotUpdate 共用 hotFields 真源）。
