// Package notifier turns Redis key-expiry events into fired reminders: it
// claims the reminder in Postgres and publishes a ReplyEvent carrying the raw
// reminder facts for consumer-reply-line-user to render and deliver (the flex
// bubble template lives there, not here). It also consumes the delivery ack
// that service publishes back, to record whether the push actually landed
// (including 429 quota exhaustion).
package notifier

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"

	"subscriber-reminder-notifier/internal/events"
	"subscriber-reminder-notifier/internal/store"
)

// FireKeyPrefix must match worker-reminder-scheduler's key scheme.
const FireKeyPrefix = "reminder:fire:"

// expiredChannel is Redis DB 0's keyspace-notification channel for expiry
// events (requires `notify-keyspace-events Ex` on the server).
const expiredChannel = "__keyevent@0__:expired"

const claimTimeout = 10 * time.Second

// Store is the subset of the Postgres store the notifier needs.
type Store interface {
	Claim(ctx context.Context, id int64) (store.Reminder, bool, error)
	Revert(ctx context.Context, id int64) error
	MarkSent(ctx context.Context, id int64) error
	MarkFailed(ctx context.Context, id int64, reason string) error
	GetDisplayName(ctx context.Context, userID string) (string, error)
}

type Notifier struct {
	store   Store
	redis   *redis.Client
	publish func(events.ReplyEvent) error
}

func New(st Store, rdb *redis.Client, nc *nats.Conn) *Notifier {
	return &Notifier{store: st, redis: rdb, publish: func(ev events.ReplyEvent) error {
		data, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		return nc.Publish(events.ReplySubject, data)
	}}
}

// Run subscribes to Redis key-expiry events and blocks until ctx is
// cancelled. Best-effort enables the keyspace-notification flag on the
// server (the deployment also sets it declaratively; this covers a manual
// CONFIG RESET or a server that predates the flux change).
func (n *Notifier) Run(ctx context.Context) error {
	if err := n.redis.ConfigSet(ctx, "notify-keyspace-events", "Ex").Err(); err != nil {
		log.Warn().Err(err).Msg("notifier: CONFIG SET notify-keyspace-events failed - relying on the server's static config")
	}

	pubsub := n.redis.Subscribe(ctx, expiredChannel)
	defer pubsub.Close()
	ch := pubsub.Channel()

	log.Info().Str("channel", expiredChannel).Msg("notifier: listening for expired reminder keys")
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-ch:
			if !ok {
				return nil
			}
			n.handleExpired(msg.Payload)
		}
	}
}

// parseFireKey extracts the reminder id from an expired key, or ok=false if
// the key doesn't belong to this pipeline (Redis fires expiry events for
// every key, not just reminder ones).
func parseFireKey(key string) (id int64, ok bool) {
	if !strings.HasPrefix(key, FireKeyPrefix) {
		return 0, false
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(key, FireKeyPrefix), 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}

func (n *Notifier) handleExpired(key string) {
	id, ok := parseFireKey(key)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), claimTimeout)
	defer cancel()

	reminder, claimed, err := n.store.Claim(ctx, id)
	if err != nil {
		log.Error().Int64("reminder_id", id).Err(err).Msg("notifier: claim failed")
		return
	}
	if !claimed {
		log.Info().Int64("reminder_id", id).Msg("notifier: already claimed - skipping (re-arm race or duplicate event)")
		return
	}

	displayName, err := n.store.GetDisplayName(ctx, reminder.CreatorID)
	if err != nil {
		log.Error().Int64("reminder_id", id).Err(err).Msg("notifier: display name lookup failed - using fallback")
	}

	err = n.publish(events.ReplyEvent{
		UserID:     reminder.TargetUserID,
		ReminderID: id,
		Reminder: &events.ReminderPayload{
			Message:            reminder.Message,
			CreatorDisplayName: displayName,
			RemindAt:           reminder.RemindAt,
		},
	})
	if err != nil {
		log.Error().Int64("reminder_id", id).Err(err).Msg("notifier: reply publish failed")
		n.revertOrFail(ctx, id, "publish_failed")
		return
	}
	log.Info().Int64("reminder_id", id).Str("target", reminder.TargetUserID).Msg("notifier: reminder published - awaiting delivery ack")
}

// revertOrFail puts the reminder back for the scheduler to re-arm when the
// failure happened before publish (nothing downstream knows about it yet);
// note the reason for anyone reading fail_reason in the meantime.
func (n *Notifier) revertOrFail(ctx context.Context, id int64, reason string) {
	if err := n.store.Revert(ctx, id); err != nil {
		log.Error().Int64("reminder_id", id).Str("reason", reason).Err(err).Msg("notifier: revert failed - reminder stuck in sending until the scheduler's stuck-sending repair fires")
	}
}

// SubscribeDelivery attaches a queue subscriber for delivery acks published
// by consumer-reply-line-user.
func (n *Notifier) SubscribeDelivery(nc *nats.Conn, queueGroup string) (*nats.Subscription, error) {
	return nc.QueueSubscribe(events.DeliverySubject, queueGroup, func(msg *nats.Msg) {
		var ack events.DeliveryEvent
		if err := json.Unmarshal(msg.Data, &ack); err != nil {
			log.Error().Str("subject", events.DeliverySubject).Err(err).Msg("notifier: bad delivery ack")
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), claimTimeout)
		defer cancel()
		n.handleDelivery(ctx, ack)
	})
}

// failReason maps a delivery error code to the reason string
// worker-reminder-scheduler's retry query matches on.
func failReason(errorCode int) string {
	if errorCode == 429 {
		return "quota_429"
	}
	return "line_" + strconv.Itoa(errorCode)
}

func (n *Notifier) handleDelivery(ctx context.Context, ack events.DeliveryEvent) {
	if ack.ReminderID == 0 {
		return // not a reminder ack
	}
	if ack.OK {
		if err := n.store.MarkSent(ctx, ack.ReminderID); err != nil {
			log.Error().Int64("reminder_id", ack.ReminderID).Err(err).Msg("notifier: mark-sent failed")
			return
		}
		log.Info().Int64("reminder_id", ack.ReminderID).Msg("notifier: reminder delivered")
		return
	}

	reason := failReason(ack.ErrorCode)
	if err := n.store.MarkFailed(ctx, ack.ReminderID, reason); err != nil {
		log.Error().Int64("reminder_id", ack.ReminderID).Err(err).Msg("notifier: mark-failed failed")
		return
	}
	log.Warn().Int64("reminder_id", ack.ReminderID).Str("reason", reason).Str("error", ack.Error).Msg("notifier: delivery failed")
}
