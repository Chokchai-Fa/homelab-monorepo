// Package consumer wires the NATS subjects to the reminder flow: reminder
// requests and postbacks drive the conversation, profile events keep the
// line_users table fresh.
package consumer

import (
	"context"
	"encoding/json"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"

	"github.com/Chokchai-Fa/homelab-monorepo/libs/natsutil"

	"consumer-reminder/internal/events"
	"consumer-reminder/internal/flow"
)

const (
	QueueGroup = "consumer-reminder"

	// handleTimeout bounds one flow step (may include one LLM extraction
	// call plus Redis/Postgres round trips).
	handleTimeout = 30 * time.Second
)

// UserStore is the subset of the store the consumer itself needs.
type UserStore interface {
	UpsertUser(ctx context.Context, userID, displayName string) error
}

// Flow is the subset of *flow.Flow the consumer drives. Extracted as an
// interface (rather than depending on *flow.Flow directly) so the message
// handlers below can be unit tested with a fake, without a live NATS/Redis.
type Flow interface {
	HandleRequest(ctx context.Context, ev events.ReminderRequestEvent)
	HandlePostback(ctx context.Context, ev events.PostbackEvent)
}

type Consumer struct {
	flow  Flow
	users UserStore
}

func New(f *flow.Flow, users UserStore) *Consumer {
	return &Consumer{flow: f, users: users}
}

// Subscribe attaches durable JetStream queue consumers for the three inbound
// subjects and returns them for shutdown cleanup. Each subject gets its own
// durable so the WorkQueue stream sees three non-overlapping consumers.
func (c *Consumer) Subscribe(js nats.JetStreamContext) ([]*nats.Subscription, error) {
	var subs []*nats.Subscription

	specs := []struct {
		subject string
		durable string
		handler nats.MsgHandler
	}{
		{events.ReminderRequestSubject, QueueGroup + "-request", c.handleReminderRequestMsg},
		{events.PostbackSubject, QueueGroup + "-postback", c.handlePostbackMsg},
		{events.ProfileSubject, QueueGroup + "-profile", c.handleProfileMsg},
	}
	for _, s := range specs {
		sub, err := js.QueueSubscribe(s.subject, s.durable, s.handler,
			natsutil.LineChatSubOpts(s.durable)...,
		)
		if err != nil {
			return nil, err
		}
		subs = append(subs, sub)
	}

	return subs, nil
}

// handleReminderRequestMsg unmarshals and validates a reminder-request
// message, then hands it to the flow. Split out from Subscribe so the
// parsing/validation/routing logic is unit-testable without a NATS
// connection: a *nats.Msg is a plain struct, no subscription required.
func (c *Consumer) handleReminderRequestMsg(msg *nats.Msg) {
	var ev events.ReminderRequestEvent
	if err := json.Unmarshal(msg.Data, &ev); err != nil {
		log.Error().Str("subject", events.ReminderRequestSubject).Err(err).Msg("consume: bad reminder request")
		msg.Ack()
		return
	}
	if ev.UserID == "" {
		log.Error().Str("subject", events.ReminderRequestSubject).Msg("consume: dropping request without user_id")
		msg.Ack()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), handleTimeout)
	defer cancel()
	log.Info().Str("user_id", ev.UserID).Msg("consume: reminder request received")
	c.flow.HandleRequest(ctx, ev)
	msg.Ack()
}

// handlePostbackMsg is handleReminderRequestMsg's counterpart for postbacks.
func (c *Consumer) handlePostbackMsg(msg *nats.Msg) {
	var ev events.PostbackEvent
	if err := json.Unmarshal(msg.Data, &ev); err != nil {
		log.Error().Str("subject", events.PostbackSubject).Err(err).Msg("consume: bad postback")
		msg.Ack()
		return
	}
	if ev.UserID == "" {
		log.Error().Str("subject", events.PostbackSubject).Msg("consume: dropping postback without user_id")
		msg.Ack()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), handleTimeout)
	defer cancel()
	log.Info().Str("user_id", ev.UserID).Str("data", ev.Data).Msg("consume: postback received")
	c.flow.HandlePostback(ctx, ev)
	msg.Ack()
}

// handleProfileMsg is handleReminderRequestMsg's counterpart for profile
// events, upserting into the user store instead of driving the flow.
func (c *Consumer) handleProfileMsg(msg *nats.Msg) {
	var ev events.ProfileEvent
	if err := json.Unmarshal(msg.Data, &ev); err != nil {
		log.Error().Str("subject", events.ProfileSubject).Err(err).Msg("consume: bad profile event")
		msg.Ack()
		return
	}
	if ev.UserID == "" || ev.DisplayName == "" {
		log.Error().Str("subject", events.ProfileSubject).Msg("consume: dropping incomplete profile event")
		msg.Ack()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), handleTimeout)
	defer cancel()
	if err := c.users.UpsertUser(ctx, ev.UserID, ev.DisplayName); err != nil {
		log.Error().Str("user_id", ev.UserID).Err(err).Msg("consume: profile upsert failed")
		// Leave unacked: a transient DB failure should redeliver rather than
		// silently drop the profile update.
		return
	}
	log.Info().Str("user_id", ev.UserID).Str("display_name", ev.DisplayName).Msg("consume: profile upserted")
	msg.Ack()
}
