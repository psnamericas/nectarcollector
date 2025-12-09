package output

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
)

// Event types - these are the discrete events we publish
const (
	EventServiceStart    = "service_start"
	EventServiceStop     = "service_stop"
	EventUncleanShutdown = "unclean_shutdown" // Previous run didn't stop cleanly (power loss, crash, reboot)
	EventStateChange     = "state_change"
	EventSignalLost      = "signal_lost"
	EventSignalDetected  = "signal_detected"
	EventReconnect       = "reconnect"
	EventBaudDetected    = "baud_detected"
	EventError           = "error"
)

// Event is the base structure for all events published to NATS.
// Keep it simple and flat for easy querying.
type Event struct {
	Timestamp  time.Time      `json:"ts"`
	Type       string         `json:"type"`
	InstanceID string         `json:"instance"`
	Channel    string         `json:"ch,omitempty"`      // A-designation (A1, A2, etc)
	Device     string         `json:"dev,omitempty"`     // /dev/ttyS1, etc
	Message    string         `json:"msg,omitempty"`     // Human-readable message
	Details    map[string]any `json:"details,omitempty"` // Optional extra data
}

// EventCallback is the function signature for event handlers.
// Channels call this when events occur; they don't know about NATS.
type EventCallback func(event Event)

// EventPublisher publishes discrete events to NATS JetStream.
// It's designed to be optional - if nil, nothing breaks.
type EventPublisher struct {
	conn       *nats.Conn
	subject    string
	instanceID string
	logger     *slog.Logger
}

// EventPublisherConfig contains configuration for EventPublisher
type EventPublisherConfig struct {
	Conn       *nats.Conn
	Subject    string // e.g., "ne.events.psna-ne-kearney-01"
	InstanceID string
	Logger     *slog.Logger
}

// NewEventPublisher creates a new EventPublisher.
// Returns nil if conn is nil (disabled mode).
func NewEventPublisher(cfg *EventPublisherConfig) *EventPublisher {
	if cfg == nil || cfg.Conn == nil {
		return nil
	}

	return &EventPublisher{
		conn:       cfg.Conn,
		subject:    cfg.Subject,
		instanceID: cfg.InstanceID,
		logger:     cfg.Logger,
	}
}

// Publish sends an event to NATS. Safe to call on nil receiver.
func (e *EventPublisher) Publish(event Event) {
	if e == nil || e.conn == nil || !e.conn.IsConnected() {
		return
	}

	// Fill in defaults
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	if event.InstanceID == "" {
		event.InstanceID = e.instanceID
	}

	data, err := json.Marshal(event)
	if err != nil {
		e.logger.Error("Failed to marshal event", "error", err, "type", event.Type)
		return
	}

	if err := e.conn.Publish(e.subject, data); err != nil {
		e.logger.Warn("Failed to publish event", "error", err, "type", event.Type)
		return
	}

	e.logger.Debug("Published event",
		"type", event.Type,
		"channel", event.Channel,
		"message", event.Message)
}

// PublishServiceStart publishes a service start event
func (e *EventPublisher) PublishServiceStart(version string) {
	e.Publish(Event{
		Type:    EventServiceStart,
		Message: "NectarCollector service started",
		Details: map[string]any{"version": version},
	})
}

// PublishServiceStop publishes a service stop event
func (e *EventPublisher) PublishServiceStop(reason string) {
	e.Publish(Event{
		Type:    EventServiceStop,
		Message: "NectarCollector service stopping",
		Details: map[string]any{"reason": reason},
	})
}

// PublishStateChange publishes a channel state change event
func (e *EventPublisher) PublishStateChange(channel, device, oldState, newState string) {
	e.Publish(Event{
		Type:    EventStateChange,
		Channel: channel,
		Device:  device,
		Message: oldState + " -> " + newState,
		Details: map[string]any{
			"old_state": oldState,
			"new_state": newState,
		},
	})
}

// PublishSignalLost publishes a signal lost event
func (e *EventPublisher) PublishSignalLost(channel, device string) {
	e.Publish(Event{
		Type:    EventSignalLost,
		Channel: channel,
		Device:  device,
		Message: "RS-232 signal lost - cable may be disconnected",
	})
}

// PublishSignalDetected publishes a signal detected event
func (e *EventPublisher) PublishSignalDetected(channel, device string) {
	e.Publish(Event{
		Type:    EventSignalDetected,
		Channel: channel,
		Device:  device,
		Message: "RS-232 signal detected - cable connected",
	})
}

// PublishReconnect publishes a reconnection attempt event
func (e *EventPublisher) PublishReconnect(channel, device string, attempt int, reason string) {
	e.Publish(Event{
		Type:    EventReconnect,
		Channel: channel,
		Device:  device,
		Message: reason,
		Details: map[string]any{"attempt": attempt},
	})
}

// PublishBaudDetected publishes a baud rate detection event
func (e *EventPublisher) PublishBaudDetected(channel, device string, baudRate int) {
	e.Publish(Event{
		Type:    EventBaudDetected,
		Channel: channel,
		Device:  device,
		Message: "Baud rate auto-detected",
		Details: map[string]any{"baud_rate": baudRate},
	})
}

// PublishError publishes an error event
func (e *EventPublisher) PublishError(channel, device, errMsg string) {
	e.Publish(Event{
		Type:    EventError,
		Channel: channel,
		Device:  device,
		Message: errMsg,
	})
}

// BuildEventsSubject constructs the events subject from state prefix and hostname
// Format: {state}.events.{hostname}
func BuildEventsSubject(subjectPrefix, instanceID string) string {
	// subjectPrefix is like "ne.cdr", we want "ne.events.{instance}"
	state := subjectPrefix
	for i, c := range subjectPrefix {
		if c == '.' {
			state = subjectPrefix[:i]
			break
		}
	}
	return state + ".events." + instanceID
}

// CheckAndPublishUncleanShutdown checks if the previous run ended without a service_stop event.
// If so, it publishes an unclean_shutdown event. Call this right after creating the EventPublisher.
func (e *EventPublisher) CheckAndPublishUncleanShutdown() {
	if e == nil || e.conn == nil {
		return
	}

	js, err := e.conn.JetStream()
	if err != nil {
		e.logger.Debug("JetStream not available for unclean shutdown check", "error", err)
		return
	}

	// Get the last message for our subject from the events stream
	sub, err := js.PullSubscribe(
		e.subject,
		"",
		nats.DeliverLast(),
		nats.BindStream("events"),
	)
	if err != nil {
		e.logger.Debug("Could not subscribe to check last event", "error", err)
		return
	}
	defer sub.Unsubscribe()

	// Try to fetch the last message with a short timeout
	msgs, err := sub.Fetch(1, nats.MaxWait(2*time.Second))
	if err != nil || len(msgs) == 0 {
		// No previous events - this is a fresh start, nothing to report
		e.logger.Debug("No previous events found - clean start")
		return
	}

	// Parse the last event
	var lastEvent Event
	if err := json.Unmarshal(msgs[0].Data, &lastEvent); err != nil {
		e.logger.Debug("Could not parse last event", "error", err)
		msgs[0].Ack()
		return
	}
	msgs[0].Ack()

	// Check if it was a clean shutdown
	if lastEvent.Type == EventServiceStop {
		e.logger.Debug("Previous run ended cleanly")
		return
	}

	// Previous run didn't end cleanly - publish unclean shutdown event
	e.logger.Warn("Previous run did not shut down cleanly",
		"last_event_type", lastEvent.Type,
		"last_event_time", lastEvent.Timestamp)

	e.Publish(Event{
		Type:    EventUncleanShutdown,
		Message: "Previous run ended unexpectedly (power loss, crash, or system reboot)",
		Details: map[string]any{
			"last_event_type": lastEvent.Type,
			"last_event_time": lastEvent.Timestamp,
		},
	})
}
