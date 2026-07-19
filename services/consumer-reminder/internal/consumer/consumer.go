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

type Consumer struct {
	flow  *flow.Flow
	users UserStore
}

func New(f *flow.Flow, users UserStore) *Consumer {
	return &Consumer{flow: f, users: users}
}

// Subscribe attaches queue subscribers for the three inbound subjects and
// returns them for shutdown cleanup.
func (c *Consumer) Subscribe(nc *nats.Conn) ([]*nats.Subscription, error) {
	var subs []*nats.Subscription

	sub, err := nc.QueueSubscribe(events.ReminderRequestSubject, QueueGroup, func(msg *nats.Msg) {
		var ev events.ReminderRequestEvent
		if err := json.Unmarshal(msg.Data, &ev); err != nil {
			log.Error().Str("subject", events.ReminderRequestSubject).Err(err).Msg("consume: bad reminder request")
			return
		}
		if ev.UserID == "" {
			log.Error().Str("subject", events.ReminderRequestSubject).Msg("consume: dropping request without user_id")
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), handleTimeout)
		defer cancel()
		log.Info().Str("user_id", ev.UserID).Msg("consume: reminder request received")
		c.flow.HandleRequest(ctx, ev)
	})
	if err != nil {
		return nil, err
	}
	subs = append(subs, sub)

	sub, err = nc.QueueSubscribe(events.PostbackSubject, QueueGroup, func(msg *nats.Msg) {
		var ev events.PostbackEvent
		if err := json.Unmarshal(msg.Data, &ev); err != nil {
			log.Error().Str("subject", events.PostbackSubject).Err(err).Msg("consume: bad postback")
			return
		}
		if ev.UserID == "" {
			log.Error().Str("subject", events.PostbackSubject).Msg("consume: dropping postback without user_id")
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), handleTimeout)
		defer cancel()
		log.Info().Str("user_id", ev.UserID).Str("data", ev.Data).Msg("consume: postback received")
		c.flow.HandlePostback(ctx, ev)
	})
	if err != nil {
		return nil, err
	}
	subs = append(subs, sub)

	sub, err = nc.QueueSubscribe(events.ProfileSubject, QueueGroup, func(msg *nats.Msg) {
		var ev events.ProfileEvent
		if err := json.Unmarshal(msg.Data, &ev); err != nil {
			log.Error().Str("subject", events.ProfileSubject).Err(err).Msg("consume: bad profile event")
			return
		}
		if ev.UserID == "" || ev.DisplayName == "" {
			log.Error().Str("subject", events.ProfileSubject).Msg("consume: dropping incomplete profile event")
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), handleTimeout)
		defer cancel()
		if err := c.users.UpsertUser(ctx, ev.UserID, ev.DisplayName); err != nil {
			log.Error().Str("user_id", ev.UserID).Err(err).Msg("consume: profile upsert failed")
			return
		}
		log.Info().Str("user_id", ev.UserID).Str("display_name", ev.DisplayName).Msg("consume: profile upserted")
	})
	if err != nil {
		return nil, err
	}
	subs = append(subs, sub)

	return subs, nil
}
