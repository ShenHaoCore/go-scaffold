package model

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"micro-scaffold/pkg/logger"

	rmq "github.com/apache/rocketmq-clients/golang/v5"
	"github.com/apache/rocketmq-clients/golang/v5/credentials"
	"github.com/zeromicro/go-zero/core/logx"
)

// MQProducer 阿里云 RocketMQ 5.x 生产者（实现 repo.MQProducer / sdkinterfaces.MQProducer）。
type MQProducer struct {
	producer       rmq.Producer
	endpoint       string
	defaultTopic   string
	group          string
	hostConfigured bool
	initErr        error
	mu             sync.RWMutex
	sendWG         sync.WaitGroup // 在途 Send；Close 等待其完成后再 GracefulStop
}

// MQOptions 组装生产者所需配置。
type MQOptions struct {
	Endpoint        string
	InstanceID      string // NameSpace；空则从 Endpoint 推导
	Topic           string
	Group           string // 消费组；生产者侧仅记录，供后续 Consumer 使用
	AccessKey       string // 实例用户名；VPC 免密可空
	AccessKeySecret string
}

// NewMQProducer 创建并 Start 生产者。Endpoint 为空 → 未配置。
// Topic 建议必填（WithTopics）；缺 Topic 时仍允许建连，Send 须显式传 topic。
func NewMQProducer(opt MQOptions) *MQProducer {
	opt.Endpoint = strings.TrimSpace(opt.Endpoint)
	opt.Topic = strings.TrimSpace(opt.Topic)
	opt.Group = strings.TrimSpace(opt.Group)
	opt.InstanceID = strings.TrimSpace(opt.InstanceID)
	opt.AccessKey = strings.TrimSpace(opt.AccessKey)
	opt.AccessKeySecret = strings.TrimSpace(opt.AccessKeySecret)

	if opt.Endpoint == "" {
		return &MQProducer{}
	}

	instance := opt.InstanceID
	if instance == "" {
		instance = DeriveRocketMQInstanceID(opt.Endpoint)
	}

	cfg := &rmq.Config{
		Endpoint:  opt.Endpoint,
		NameSpace: instance,
		Credentials: &credentials.SessionCredentials{
			AccessKey:    opt.AccessKey,
			AccessSecret: opt.AccessKeySecret,
		},
	}
	if opt.Group != "" {
		cfg.ConsumerGroup = opt.Group
	}

	var opts []rmq.ProducerOption
	if opt.Topic != "" {
		opts = append(opts, rmq.WithTopics(opt.Topic))
	}

	p, err := rmq.NewProducer(cfg, opts...)
	if err != nil {
		logx.Errorf("mq producer init failed: %s", logger.RedactString(err.Error()))
		return &MQProducer{hostConfigured: true, endpoint: opt.Endpoint, defaultTopic: opt.Topic, group: opt.Group, initErr: err}
	}
	if err := p.Start(); err != nil {
		logx.Errorf("mq producer start failed: %s", logger.RedactString(err.Error()))
		_ = p.GracefulStop()
		return &MQProducer{hostConfigured: true, endpoint: opt.Endpoint, defaultTopic: opt.Topic, group: opt.Group, initErr: err}
	}
	return &MQProducer{
		producer:       p,
		endpoint:       opt.Endpoint,
		defaultTopic:   opt.Topic,
		group:          opt.Group,
		hostConfigured: true,
	}
}

// DeriveRocketMQInstanceID 从接入点推导实例 ID。
// 例：rmq-cn-xxxxxxxxx-vpc.<region>.rmq.aliyuncs.com:8080 → rmq-cn-xxxxxxxxx
func DeriveRocketMQInstanceID(endpoint string) string {
	host := strings.TrimSpace(endpoint)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	first := host
	if i := strings.IndexByte(host, '.'); i > 0 {
		first = host[:i]
	}
	return strings.TrimSuffix(first, "-vpc")
}

func (m *MQProducer) Configured() bool {
	return m != nil && m.hostConfigured
}

func (m *MQProducer) DefaultTopic() string {
	if m == nil {
		return ""
	}
	return m.defaultTopic
}

func (m *MQProducer) Group() string {
	if m == nil {
		return ""
	}
	return m.group
}

func (m *MQProducer) Ping(ctx context.Context) error {
	if m == nil || !m.hostConfigured {
		return fmt.Errorf("mq not configured")
	}
	m.mu.RLock()
	initErr := m.initErr
	p := m.producer
	endpoint := m.endpoint
	m.mu.RUnlock()
	if initErr != nil {
		return initErr
	}
	if p == nil {
		return fmt.Errorf("mq producer closed")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// SDK 无专用 Ping：探测 Endpoint TCP 可达，避免仅检查本地 producer 非 nil。
	if endpoint != "" {
		if err := dialMQEndpoint(ctx, endpoint); err != nil {
			return fmt.Errorf("mq ping: %w", err)
		}
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func dialMQEndpoint(ctx context.Context, endpoint string) error {
	addr := strings.TrimSpace(endpoint)
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(addr, "8080")
	}
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}

func (m *MQProducer) Send(ctx context.Context, topic string, body []byte) error {
	if m == nil || !m.hostConfigured {
		return fmt.Errorf("mq not configured")
	}
	m.mu.RLock()
	initErr := m.initErr
	p := m.producer
	defaultTopic := m.defaultTopic
	if initErr != nil {
		m.mu.RUnlock()
		return initErr
	}
	if p == nil {
		m.mu.RUnlock()
		return fmt.Errorf("mq producer closed")
	}
	// Add 须与 p==nil 判定同一把锁：Close 置 nil 用写锁，保证 Wait 之后不再有新 Add
	m.sendWG.Add(1)
	m.mu.RUnlock()
	defer m.sendWG.Done()

	topic = strings.TrimSpace(topic)
	if topic == "" {
		topic = defaultTopic
	}
	if topic == "" {
		return fmt.Errorf("mq topic required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	msg := &rmq.Message{Topic: topic, Body: body}
	_, err := p.Send(ctx, msg)
	if err != nil {
		return fmt.Errorf("mq send: %w", err)
	}
	return nil
}

func (m *MQProducer) Close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	p := m.producer
	m.producer = nil
	m.mu.Unlock()
	if p == nil {
		return nil
	}
	// 先置 nil 再等待在途 Send 完成，最后 GracefulStop：避免 Stop 与在途 Send 并发
	m.sendWG.Wait()
	return p.GracefulStop()
}
