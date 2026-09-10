// Package mq RabbitMQ 封装（T4.2）：拓扑声明 + 可靠投递（confirm 模式）。
//
// 拓扑：
//
//	exchange pluto.events (direct) ──like.created/like.removed/
//	                                 comment.created/comment.deleted──▶ queue pluto.interaction
//	  pluto.interaction 带 x-dead-letter-exchange=pluto.dlx（NACK 不重入队 → 进死信）
//	exchange pluto.dlx (fanout) ──▶ queue pluto.dlq（死信兜底）
//
// 可靠投递三件套：durable 队列 + Persistent 消息 + publisher confirm。
package mq

import (
	"context"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"pluto_feed/internal/model"
)

// 拓扑常量（集中一处）。
const (
	Exchange = "pluto.events"
	DLX      = "pluto.dlx"
	Queue    = "pluto.interaction"
	DLQ      = "pluto.dlq"
)

// 路由键复用 model 的事件类型常量（event_type == routing key，同源防漂移）。
var (
	RkLikeCreated   = model.EventLikeCreated
	RkLikeRemoved   = model.EventLikeRemoved
	RkCommentCreate = model.EventCommentCreate
	RkCommentDelete = model.EventCommentDelete
)

// Connect 建立 AMQP 连接。启动期 fail-fast：Worker 连不上 MQ 就不该起来。
func Connect(url string) (*amqp.Connection, error) {
	return amqp.Dial(url)
}

// SetupTopology 声明交换机/队列/绑定。所有声明都是幂等的，可重复执行。
// 队列参数一经声明不可变更（rabbitmq 会报错），改参数需先删队列——生产事故高发点，注释留档。
func SetupTopology(conn *amqp.Connection) (*amqp.Channel, error) {
	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("open channel: %w", err)
	}

	// 业务交换机
	if err := ch.ExchangeDeclare(Exchange, "direct", true, false, false, false, nil); err != nil {
		return nil, fmt.Errorf("declare %s: %w", Exchange, err)
	}
	// 死信交换机（fanout：死信广播给所有绑定队列，目前只有 dlq）
	if err := ch.ExchangeDeclare(DLX, "fanout", true, false, false, false, nil); err != nil {
		return nil, fmt.Errorf("declare %s: %w", DLX, err)
	}

	// 业务队列：声明时挂上死信参数
	if _, err := ch.QueueDeclare(Queue, true, false, false, false, amqp.Table{
		"x-dead-letter-exchange": DLX,
	}); err != nil {
		return nil, fmt.Errorf("declare %s: %w", Queue, err)
	}
	for _, rk := range []string{RkLikeCreated, RkLikeRemoved, RkCommentCreate, RkCommentDelete} {
		if err := ch.QueueBind(Queue, rk, Exchange, false, nil); err != nil {
			return nil, fmt.Errorf("bind %s: %w", rk, err)
		}
	}

	// 死信队列
	if _, err := ch.QueueDeclare(DLQ, true, false, false, false, nil); err != nil {
		return nil, fmt.Errorf("declare %s: %w", DLQ, err)
	}
	if err := ch.QueueBind(DLQ, "", DLX, false, nil); err != nil {
		return nil, fmt.Errorf("bind %s: %w", DLQ, err)
	}
	return ch, nil
}

// Publisher confirm 模式的发布者：每条消息等 broker 确认，确认成功才算"投递成功"。
type Publisher struct {
	ch       *amqp.Channel
	confirms <-chan amqp.Confirmation
}

// NewPublisher 开启 confirm 模式（Once=true 表示全 channel 生效）。
func NewPublisher(conn *amqp.Connection) (*Publisher, error) {
	ch, err := SetupTopology(conn)
	if err != nil {
		return nil, err
	}
	if err := ch.Confirm(false); err != nil {
		return nil, fmt.Errorf("confirm mode: %w", err)
	}
	return &Publisher{ch: ch, confirms: ch.NotifyPublish(make(chan amqp.Confirmation, 1))}, nil
}

// Channel 暴露底层 channel 供 consumer/dlq 复用（worker 三角色共用一条连接）。
func (p *Publisher) Channel() *amqp.Channel { return p.ch }

// Publish 可靠投递：MessageId = outbox.id（消费端幂等键），持久化消息。
// 等待 broker 确认（2 秒超时），未确认 = 投递失败 = 不标记 outbox → 下轮重投。
func (p *Publisher) Publish(ctx context.Context, eventID uint64, eventType string, payload []byte) error {
	if err := p.ch.PublishWithContext(ctx, Exchange, eventType, false, false, amqp.Publishing{
		MessageId:    fmt.Sprintf("%d", eventID),
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent, // 持久化消息 + durable 队列 = broker 重启不丢
		Timestamp:    time.Now(),
		Body:         payload,
	}); err != nil {
		return fmt.Errorf("publish: %w", err)
	}
	select {
	case c, ok := <-p.confirms:
		if !ok {
			return fmt.Errorf("confirm channel closed")
		}
		if !c.Ack {
			return fmt.Errorf("broker nacked message %d", eventID)
		}
		return nil
	case <-time.After(2 * time.Second):
		return fmt.Errorf("confirm timeout for message %d", eventID)
	}
}
