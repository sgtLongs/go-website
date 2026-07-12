package realtime

type Event struct {
	Type   string `json:"type"`
	RoomID string `json:"roomId"`
	Data   any    `json:"data"`
}

type Participant struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type presenceSnapshot struct {
	Participants []Participant `json:"participants"`
}
