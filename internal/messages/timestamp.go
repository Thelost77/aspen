package messages

import "time"

const appleEpochOffsetSeconds int64 = 978307200

// AppleTime converts a Messages timestamp to time.Time. Current Messages
// databases store nanoseconds since 2001; older databases may store seconds.
func AppleTime(value int64) (time.Time, bool) {
	if value == 0 {
		return time.Time{}, false
	}

	magnitude := value
	if magnitude < 0 {
		magnitude = -magnitude
	}
	if magnitude >= 1_000_000_000_000 {
		seconds := value / int64(time.Second)
		nanoseconds := value % int64(time.Second)
		return time.Unix(seconds+appleEpochOffsetSeconds, nanoseconds), true
	}
	return time.Unix(value+appleEpochOffsetSeconds, 0), true
}
