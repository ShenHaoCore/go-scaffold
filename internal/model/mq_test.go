package model

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDeriveRocketMQInstanceID(t *testing.T) {
	require.Equal(t, "rmq-cn-xxxxxxxxx", DeriveRocketMQInstanceID("rmq-cn-xxxxxxxxx-vpc.cn-shenzhen.rmq.aliyuncs.com:8080"))
	require.Equal(t, "rmq-cn-xxxxxxxxx", DeriveRocketMQInstanceID("rmq-cn-xxxxxxxxx.cn-shenzhen.rmq.aliyuncs.com:8080"))
}

func TestNewMQProducer_Empty(t *testing.T) {
	p := NewMQProducer(MQOptions{})
	require.False(t, p.Configured())
	require.Error(t, p.Ping(nil))
}

func TestMQProducer_CloseNilSafe(t *testing.T) {
	var p *MQProducer
	require.NoError(t, p.Close())
}

func TestDialMQEndpoint_BadAddr(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := dialMQEndpoint(ctx, "127.0.0.1:1")
	require.Error(t, err)
}
