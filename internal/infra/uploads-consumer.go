package infra

import (
	"encoding/json"
	"log"
	"time"

	"github.com/nats-io/nats.go"
)

type UploadEvent struct {
	OriginalName string    `json:"original_name"`
	StoredName   string    `json:"stored_name"`
	StoredPath   string    `json:"stored_path"`
	Size         int64     `json:"size"`
	UploadedAt   time.Time `json:"uploaded_at"`
	UserAgent    *string   `json:"user_agent,omitempty"`
	RemoteAddr   *string   `json:"remote_addr,omitempty"`
}

func SubscribeUploads(js nats.JetStreamContext, subject, durable string, cb func(hdrMsgID string, ev UploadEvent) error) (*nats.Subscription, error) {
	handler := func(m *nats.Msg) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("panic in upload handler: %v", r)
			}
		}()

		var ev UploadEvent
		if err := json.Unmarshal(m.Data, &ev); err != nil {
			log.Printf("json decode error: %v", err)
			_ = m.Term()
			return
		}

		msgID := m.Header.Get("Msg-Id")

		if err := cb(msgID, ev); err != nil {
			log.Printf("handler error for %q: %v", msgID, err)
			_ = m.Nak()
			return
		}
		_ = m.Ack()
	}

	sub, err := js.Subscribe(
		subject,
		handler,
		nats.Durable(durable),
		nats.DeliverNew(),
		nats.ManualAck(),
		nats.AckWait(30*time.Second),
		nats.MaxAckPending(1024),
	)
	if err != nil {
		return nil, err
	}
	return sub, nil
}
