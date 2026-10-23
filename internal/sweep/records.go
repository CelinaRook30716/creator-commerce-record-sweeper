package sweep

import "time"

type Kind string

const (
	DigitalDelivery   Kind = "digital_asset_delivery"
	SubscriberUpdate  Kind = "subscriber_update"
	ContentProcessing Kind = "content_processing"
)

type Record struct {
	ID        string    `json:"id"`
	Kind      Kind      `json:"kind"`
	ObjectKey string    `json:"object_key"`
	State     string    `json:"state"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SelectStale keeps active work and applies a retention window to terminal states.
func SelectStale(records []Record, now time.Time) []Record {
	retention := map[Kind]time.Duration{
		DigitalDelivery:   72 * time.Hour,
		SubscriberUpdate:  24 * time.Hour,
		ContentProcessing: 6 * time.Hour,
	}
	terminal := map[Kind]string{
		DigitalDelivery:   "delivered",
		SubscriberUpdate:  "sent",
		ContentProcessing: "completed",
	}

	var stale []Record
	for _, record := range records {
		window, known := retention[record.Kind]
		if known && record.State == terminal[record.Kind] && !record.UpdatedAt.After(now.Add(-window)) {
			stale = append(stale, record)
		}
	}
	return stale
}
