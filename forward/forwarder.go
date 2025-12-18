package forward

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"nectarcollector/config"

	"github.com/nats-io/nats.go"
)

// Forwarder pulls from local JetStream, pushes to remote NATS.
type Forwarder struct {
	cfg        *config.ForwarderConfig
	instanceID string
	localConn  *nats.Conn
	remoteConn *nats.Conn
	sub        *nats.Subscription
	logger     *slog.Logger

	mu        sync.Mutex
	forwarded int64

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

type ForwarderConfig struct {
	Config     *config.ForwarderConfig
	InstanceID string
	LocalConn  *nats.Conn
	Logger     *slog.Logger
}

type Stats struct {
	Enabled   bool  `json:"enabled"`
	Connected bool  `json:"connected"`
	Forwarded int64 `json:"forwarded"`
}

func New(cfg *ForwarderConfig) *Forwarder {
	return &Forwarder{
		cfg:        cfg.Config,
		instanceID: cfg.InstanceID,
		localConn:  cfg.LocalConn,
		logger:     cfg.Logger,
	}
}

func (f *Forwarder) Start(ctx context.Context) error {
	if !f.cfg.Enabled {
		return nil
	}

	f.ctx, f.cancel = context.WithCancel(ctx)

	// Connect to remote
	opts := []nats.Option{
		nats.Name(f.instanceID + "-forwarder"),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(5 * time.Second),
	}
	if f.cfg.RemoteCreds != "" {
		opts = append(opts, nats.UserCredentials(f.cfg.RemoteCreds))
	}
	var err error
	f.remoteConn, err = nats.Connect(f.cfg.RemoteURL, opts...)
	if err != nil {
		return fmt.Errorf("remote NATS: %w", err)
	}

	// Setup durable consumer on local cdr stream
	js, err := f.localConn.JetStream()
	if err != nil {
		f.remoteConn.Close()
		return fmt.Errorf("local JetStream: %w", err)
	}

	name := f.instanceID + "-forwarder"
	if _, err := js.ConsumerInfo("cdr", name); errors.Is(err, nats.ErrConsumerNotFound) {
		_, err = js.AddConsumer("cdr", &nats.ConsumerConfig{
			Durable:       name,
			AckPolicy:     nats.AckExplicitPolicy,
			AckWait:       30 * time.Second,
			MaxDeliver:    -1,
			MaxAckPending: 1,
			DeliverPolicy: nats.DeliverAllPolicy,
		})
		if err != nil {
			f.remoteConn.Close()
			return fmt.Errorf("create consumer: %w", err)
		}
	}

	f.sub, err = js.PullSubscribe("", name, nats.Bind("cdr", name))
	if err != nil {
		f.remoteConn.Close()
		return fmt.Errorf("subscribe: %w", err)
	}

	f.wg.Add(1)
	go f.run()

	f.logger.Info("Forwarder started", "remote", f.cfg.RemoteURL)
	return nil
}

func (f *Forwarder) Stop() {
	if f.cancel == nil {
		return
	}
	f.cancel()
	f.wg.Wait()
	if f.sub != nil {
		f.sub.Unsubscribe()
	}
	if f.remoteConn != nil {
		f.remoteConn.Close()
	}
	f.logger.Info("Forwarder stopped", "forwarded", f.forwarded)
}

func (f *Forwarder) Stats() Stats {
	f.mu.Lock()
	fwd := f.forwarded
	f.mu.Unlock()
	return Stats{
		Enabled:   f.cfg.Enabled,
		Connected: f.remoteConn != nil && f.remoteConn.IsConnected(),
		Forwarded: fwd,
	}
}

func (f *Forwarder) run() {
	defer f.wg.Done()

	subject := f.cfg.RemoteSubject

	for {
		select {
		case <-f.ctx.Done():
			return
		default:
		}

		if !f.remoteConn.IsConnected() {
			time.Sleep(time.Second)
			continue
		}

		msgs, err := f.sub.Fetch(1, nats.MaxWait(2*time.Second))
		if err != nil || len(msgs) == 0 {
			continue
		}

		msg := msgs[0]
		err = f.remoteConn.Publish(subject, msg.Data)
		if err == nil {
			err = f.remoteConn.Flush()
		}
		if err != nil {
			msg.Nak()
			continue
		}

		msg.Ack()
		f.mu.Lock()
		f.forwarded++
		f.mu.Unlock()
	}
}
