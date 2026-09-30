package model

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"go-scaffold/pkg/env"
	"go-scaffold/pkg/logger"

	sls "github.com/aliyun/aliyun-log-go-sdk"
	"github.com/gogo/protobuf/proto"
	"github.com/zeromicro/go-zero/core/logx"
)

const (
	slsHTTPTimeout  = 5 * time.Second
	slsRetryTimeout = 5 * time.Second
	slsMaxAsync     = 64
	// Close 须盖住 HTTP+重试上界，避免仍持有 cli 的 in-flight 与 cli.Close 竞态
	slsCloseWait = slsHTTPTimeout + slsRetryTimeout + 2*time.Second
)

// SLSWriter 阿里云 SLS 写入与探活（实现 repo.SLSClient / sdkinterfaces.SLSWriter）。
// 五项齐备才启用；半配置视为未启用（health skipped），与 OSS 一致。
type SLSWriter struct {
	client         sls.ClientInterface
	project        string
	logstore       string
	topic          string
	source         string
	hostConfigured bool
	initErr        error
	mu             sync.Mutex
	closed         bool
	asyncSem       chan struct{}
	asyncWG        sync.WaitGroup
	inflight       sync.WaitGroup // Ping / Write / WriteFields（含异步路径）
}

// SLSOptions 组装 SLS 客户端。
type SLSOptions struct {
	Endpoint        string // 如 <region>-internal.log.aliyuncs.com
	Project         string // 如 <project>
	Logstore        string // 如 <logstore>
	AccessKeyID     string
	AccessKeySecret string
	Topic           string // 可选；默认空
	Source          string // 可选；默认主机名
}

// NewSLSWriter 创建 SLS Writer。未配齐 → Configured=false。
func NewSLSWriter(opt SLSOptions) *SLSWriter {
	opt.Endpoint = strings.TrimSpace(opt.Endpoint)
	opt.Project = strings.TrimSpace(opt.Project)
	opt.Logstore = strings.TrimSpace(opt.Logstore)
	opt.AccessKeyID = strings.TrimSpace(opt.AccessKeyID)
	opt.AccessKeySecret = strings.TrimSpace(opt.AccessKeySecret)
	opt.Topic = strings.TrimSpace(opt.Topic)
	opt.Source = strings.TrimSpace(opt.Source)

	if opt.Endpoint == "" && opt.Project == "" && opt.Logstore == "" && opt.AccessKeyID == "" && opt.AccessKeySecret == "" {
		return &SLSWriter{}
	}
	if opt.Endpoint == "" || opt.Project == "" || opt.Logstore == "" || opt.AccessKeyID == "" || opt.AccessKeySecret == "" {
		logx.Infof("sls: incomplete config ignored (need SLS_ENDPOINT, SLS_PROJECT, SLS_LOGSTORE, SLS_ACCESS_KEY_ID, SLS_ACCESS_KEY_SECRET)")
		return &SLSWriter{}
	}

	endpoint, err := normalizeSLSEndpoint(opt.Endpoint)
	if err != nil {
		return &SLSWriter{hostConfigured: true, initErr: err}
	}
	cli := sls.CreateNormalInterface(endpoint, opt.AccessKeyID, opt.AccessKeySecret, "")
	// 限制单次 HTTP / 重试总时长，避免 Ping/PutLogs 在 ctx 取消后仍长期占用 goroutine
	cli.SetHTTPClient(&http.Client{Timeout: slsHTTPTimeout})
	cli.SetRetryTimeout(slsRetryTimeout)
	src := opt.Source
	if src == "" {
		src, _ = os.Hostname()
	}
	return &SLSWriter{
		client:         cli,
		project:        opt.Project,
		logstore:       opt.Logstore,
		topic:          opt.Topic,
		source:         src,
		hostConfigured: true,
		asyncSem:       make(chan struct{}, slsMaxAsync),
	}
}

// normalizeSLSEndpoint 补全 https://；prod 禁止明文 http（与 OSS 的 SLS_ALLOW_INSECURE 门禁对称）。
func normalizeSLSEndpoint(endpoint string) (string, error) {
	endpoint = strings.TrimSpace(endpoint)
	lower := strings.ToLower(endpoint)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		if strings.HasPrefix(lower, "http://") && env.IsProd() && !slsInsecureAllowed() {
			return "", fmt.Errorf("sls: http endpoint forbidden when APP_ENV=prod (use https://, or SLS_ALLOW_INSECURE=true for break-glass)")
		}
		if strings.HasPrefix(lower, "http://") && env.IsProd() && slsInsecureAllowed() {
			logx.Severe("SECURITY: SLS_ALLOW_INSECURE=true with http endpoint in prod (break-glass; prefer https)")
		}
		return endpoint, nil
	}
	// 内网 Endpoint 默认 HTTPS
	return "https://" + endpoint, nil
}

func slsInsecureAllowed() bool {
	v := strings.TrimSpace(os.Getenv("SLS_ALLOW_INSECURE"))
	return strings.EqualFold(v, "true") || v == "1"
}

func (s *SLSWriter) Configured() bool {
	return s != nil && s.hostConfigured
}

func (s *SLSWriter) Ping(ctx context.Context) error {
	if s == nil || !s.hostConfigured {
		return fmt.Errorf("sls not configured")
	}
	if s.initErr != nil {
		return s.initErr
	}
	s.mu.Lock()
	cli := s.client
	closed := s.closed
	project, logstore := s.project, s.logstore
	if closed || cli == nil {
		s.mu.Unlock()
		return fmt.Errorf("sls writer closed")
	}
	s.inflight.Add(1)
	s.mu.Unlock()

	if ctx == nil {
		ctx = context.Background()
	}
	done := make(chan error, 1)
	go func() {
		defer s.inflight.Done()
		ok, err := cli.CheckLogstoreExist(project, logstore)
		if err != nil {
			done <- fmt.Errorf("sls check logstore: %w", err)
			return
		}
		if !ok {
			done <- fmt.Errorf("sls logstore %q not found in project %q", logstore, project)
			return
		}
		done <- nil
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

// Write 将一行日志写入 Logstore（同步 PutLogs）；实现 io.Writer / sdkinterfaces.SLSWriter。
func (s *SLSWriter) Write(p []byte) (int, error) {
	if s == nil || !s.hostConfigured {
		return 0, fmt.Errorf("sls not configured")
	}
	if s.initErr != nil {
		return 0, s.initErr
	}
	s.mu.Lock()
	cli := s.client
	closed := s.closed
	project, logstore, topic, source := s.project, s.logstore, s.topic, s.source
	if closed || cli == nil {
		s.mu.Unlock()
		return 0, fmt.Errorf("sls writer closed")
	}
	s.inflight.Add(1)
	s.mu.Unlock()
	defer s.inflight.Done()

	msg := strings.TrimRight(string(p), "\r\n")
	if msg == "" {
		return len(p), nil
	}
	now := uint32(time.Now().Unix())
	lg := &sls.LogGroup{
		Topic:  proto.String(topic),
		Source: proto.String(source),
		Logs: []*sls.Log{
			{
				Time:     proto.Uint32(now),
				Contents: buildSLSContents("info", msg, nil),
			},
		},
	}
	if err := cli.PutLogs(project, logstore, lg); err != nil {
		return 0, fmt.Errorf("sls put logs: %w", err)
	}
	return len(p), nil
}

// WriteFields 写入结构化字段（供直接调用；logx 适配器走 WriteFieldsAsync）。
// 字段约定对齐 logx JSON 文件：level / content / @timestamp / caller，外加规范字段 service / env / trace_id。
func (s *SLSWriter) WriteFields(level, message string, fields map[string]string) error {
	if s == nil || !s.hostConfigured {
		return fmt.Errorf("sls not configured")
	}
	if s.initErr != nil {
		return s.initErr
	}
	s.mu.Lock()
	cli := s.client
	closed := s.closed
	project, logstore, topic, source := s.project, s.logstore, s.topic, s.source
	if closed || cli == nil {
		s.mu.Unlock()
		return fmt.Errorf("sls writer closed")
	}
	s.inflight.Add(1)
	s.mu.Unlock()
	defer s.inflight.Done()

	now := uint32(time.Now().Unix())
	lg := &sls.LogGroup{
		Topic:  proto.String(topic),
		Source: proto.String(source),
		Logs:   []*sls.Log{{Time: proto.Uint32(now), Contents: buildSLSContents(level, message, fields)}},
	}
	if err := cli.PutLogs(project, logstore, lg); err != nil {
		return fmt.Errorf("sls put logs: %w", err)
	}
	return nil
}

// WriteFieldsAsync 有界异步写入：满载或已关闭则丢弃，避免 SLS 抖动反压业务日志路径。
func (s *SLSWriter) WriteFieldsAsync(level, message string, fields map[string]string) {
	if s == nil || !s.hostConfigured {
		return
	}
	if s.initErr != nil {
		return
	}
	var fieldsCopy map[string]string
	if len(fields) > 0 {
		fieldsCopy = make(map[string]string, len(fields))
		for k, v := range fields {
			fieldsCopy[k] = v
		}
	}
	s.mu.Lock()
	if s.closed || s.asyncSem == nil {
		s.mu.Unlock()
		return
	}
	select {
	case s.asyncSem <- struct{}{}:
		s.asyncWG.Add(1) // 须在 closed 判定同一把锁内 Add，避免 Close 排空后仍入队
		s.mu.Unlock()
	default:
		s.mu.Unlock()
		return
	}
	go func() {
		defer s.asyncWG.Done()
		defer func() { <-s.asyncSem }()
		_ = s.WriteFields(level, message, fieldsCopy)
	}()
}

// slsTimeFormat 与 config Log.TimeFormat / normalizeLogConf 默认一致（RFC3339 milli）。
const slsTimeFormat = "2006-01-02T15:04:05.000Z07:00"

// buildSLSContents 组装 SLS LogContent（扁平 KV，键唯一）。
// - 正文键用 content（对齐 go-zero JSON，便于文件日志与 SLS 同字段检索）
// - 始终写入 @timestamp / service / env
// - fields 中的 level_hint=warn 提升为 level=warn（logx 无独立 Warn）
// - fields 可覆盖同名键；空键跳过
func buildSLSContents(level, message string, fields map[string]string) []*sls.LogContent {
	level = strings.TrimSpace(level)
	if level == "" {
		level = "info"
	}
	if fields != nil {
		if hint := strings.TrimSpace(fields["level_hint"]); strings.EqualFold(hint, "warn") || strings.EqualFold(hint, "warning") {
			level = "warn"
		}
	}

	ordered := []struct{ k, v string }{
		{"@timestamp", time.Now().Format(slsTimeFormat)},
		{"level", level},
		{"content", logger.RedactString(message)},
		{"service", logger.ServiceName()},
		{"env", logger.EnvName()},
	}
	seen := map[string]int{}
	for i, kv := range ordered {
		seen[kv.k] = i
	}
	out := append([]struct{ k, v string }(nil), ordered...)

	put := func(k, v string) {
		k = strings.TrimSpace(k)
		if k == "" || k == "level_hint" {
			return
		}
		// 正文统一走 content；兼容误传 message
		if k == "message" {
			k = "content"
		}
		v = logger.RedactString(v)
		if i, ok := seen[k]; ok {
			out[i].v = v
			return
		}
		seen[k] = len(out)
		out = append(out, struct{ k, v string }{k, v})
	}

	for k, v := range fields {
		if strings.TrimSpace(k) == "level" {
			continue // level 已由参数 + level_hint 决定
		}
		put(k, v)
	}

	contents := make([]*sls.LogContent, 0, len(out))
	for _, kv := range out {
		contents = append(contents, &sls.LogContent{
			Key:   proto.String(kv.k),
			Value: proto.String(kv.v),
		})
	}
	return contents
}

func (s *SLSWriter) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	cli := s.client
	s.client = nil
	s.mu.Unlock()

	done := make(chan struct{})
	go func() {
		s.asyncWG.Wait()
		s.inflight.Wait()
		close(done)
	}()
	// 可停止的 timer：done 先到则回收计时器，避免 time.After 遗留未到期 timer
	timer := time.NewTimer(slsCloseWait)
	select {
	case <-done:
		if !timer.Stop() {
			<-timer.C
		}
	case <-timer.C:
	}

	if cli != nil {
		return cli.Close()
	}
	return nil
}

// SLSLogxWriter 将 logx 级别日志转发到 SLS（不替换原有 Mode=file Writer）。
type SLSLogxWriter struct {
	sls *SLSWriter
}

// NewSLSLogxWriter 包装 SLSWriter；未配置时返回 nil。
func NewSLSLogxWriter(w *SLSWriter) *SLSLogxWriter {
	if w == nil || !w.Configured() {
		return nil
	}
	return &SLSLogxWriter{sls: w}
}

func (w *SLSLogxWriter) Alert(v any) {
	w.write("alert", v, nil)
}
func (w *SLSLogxWriter) Close() error {
	// 生命周期由 ServiceContext 持有 SLSWriter；logx.Close（rest.Server.Stop）不得先拆掉共享客户端。
	return nil
}
func (w *SLSLogxWriter) Debug(v any, fields ...logx.LogField) {
	w.write("debug", v, fields)
}
func (w *SLSLogxWriter) Error(v any, fields ...logx.LogField) {
	w.write("error", v, fields)
}
func (w *SLSLogxWriter) Info(v any, fields ...logx.LogField) {
	w.write("info", v, fields)
}
func (w *SLSLogxWriter) Severe(v any) {
	w.write("severe", v, nil)
}
func (w *SLSLogxWriter) Slow(v any, fields ...logx.LogField) {
	w.write("slow", v, fields)
}
func (w *SLSLogxWriter) Stack(v any) {
	w.write("stack", v, nil)
}
func (w *SLSLogxWriter) Stat(v any, fields ...logx.LogField) {
	w.write("stat", v, fields)
}

func (w *SLSLogxWriter) write(level string, v any, fields []logx.LogField) {
	if w == nil || w.sls == nil {
		return
	}
	msg := fmt.Sprint(v)
	m := make(map[string]string, len(fields))
	for _, f := range fields {
		m[f.Key] = fmt.Sprint(f.Value)
	}
	// 异步有界：不把 PutLogs 阻塞叠到业务日志调用栈上
	w.sls.WriteFieldsAsync(level, msg, m)
}
