package sweep

import (
	"testing"
	"time"
)

func TestSelectStale(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		record Record
		want   bool
	}{
		{"expired delivery", Record{"delivery-41", DigitalDelivery, "deliveries/41.zip", "delivered", now.Add(-73 * time.Hour)}, true},
		{"recent delivery", Record{"delivery-42", DigitalDelivery, "deliveries/42.zip", "delivered", now.Add(-2 * time.Hour)}, false},
		{"pending subscriber update", Record{"update-7", SubscriberUpdate, "updates/7.json", "pending", now.Add(-48 * time.Hour)}, false},
		{"expired subscriber update", Record{"update-8", SubscriberUpdate, "updates/8.json", "sent", now.Add(-25 * time.Hour)}, true},
		{"expired processing output", Record{"render-9", ContentProcessing, "processing/9.mp4", "completed", now.Add(-6 * time.Hour)}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SelectStale([]Record{tt.record}, now)
			if (len(got) == 1) != tt.want {
				t.Fatalf("SelectStale() returned %d records, want selected=%v", len(got), tt.want)
			}
		})
	}
}
