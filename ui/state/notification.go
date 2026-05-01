package state

import (
	"context"
	"errors"
	"time"
)

// NotificationLevel distinguishes error notifications from success notifications.
type NotificationLevel uint8

const (
	NotificationError NotificationLevel = iota
	NotificationSuccess
)

// NotificationState holds the state for the centralized notification bar.
// Pre-allocated in Application; no allocations during the UI loop.
type NotificationState struct {
	Message string
	Level   NotificationLevel
	ShowAt  time.Time
	Active  bool
}

func (n *NotificationState) Show(msg string, level NotificationLevel, now time.Time) {
	n.Message = msg
	n.Level = level
	n.ShowAt = now
	n.Active = true
}

// IsExpired returns true if the notification has been active longer than the given duration.
func (n *NotificationState) IsExpired(now time.Time, timeout time.Duration) bool {
	return n.Active && now.Sub(n.ShowAt) >= timeout
}

// Clear dismisses the current notification.
func (n *NotificationState) Clear() {
	n.Message = ""
	n.Active = false
}

// RecordLoadOutcome routes the result of an async load to the persistent
// errSlot (when non-nil) and the transient notification bar. A
// context.DeadlineExceeded error is prefixed with "Operation timed out: "
// before either destination so generic Application-level polling and bespoke
// per-controller polling produce identical user-visible text.
func RecordLoadOutcome(err error, errSlot *string, notif *NotificationState) {
	if err == nil {
		if errSlot != nil {
			*errSlot = ""
		}

		return
	}

	msg := err.Error()
	if errors.Is(err, context.DeadlineExceeded) {
		msg = "Operation timed out: " + msg
	}

	if errSlot != nil {
		*errSlot = msg
	}

	notif.Show(msg, NotificationError, time.Now())
}
