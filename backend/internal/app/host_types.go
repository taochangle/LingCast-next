package app

import "time"

type HostAngle struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}
type HostTopic struct {
	ID        string      `json:"id"`
	AvatarID  string      `json:"avatarId"`
	Title     string      `json:"title"`
	Angles    []HostAngle `json:"angles"`
	State     string      `json:"state"`
	CreatedAt time.Time   `json:"createdAt"`
	Key       string      `json:"key"`
}
type HostDraft struct {
	TaskID    string `json:"taskId"`
	SessionID string `json:"sessionId"`
	Revision  int64  `json:"revision"`
	TopicID   string `json:"topicId"`
	AngleID   string `json:"angleId"`
	Text      string `json:"text"`
	PauseMs   int    `json:"pauseMs"`
}
type TaskResult struct {
	Seq         int64     `json:"seq"`
	TaskID      string    `json:"taskId"`
	State       string    `json:"state"`
	Text        string    `json:"text"`
	CompletedAt time.Time `json:"completedAt"`
}
type ViewerTurn struct {
	Kind     string `json:"kind"`
	Nickname string `json:"nickname"`
	Text     string `json:"text"`
}
type SpokenTurn struct {
	TaskID string `json:"taskId"`
	Source string `json:"source"`
	Text   string `json:"text"`
}
type HostInput struct {
	CurrentAngleID  string       `json:"currentAngleId"`
	AvatarID        string       `json:"avatarId"`
	SessionID       string       `json:"sessionId"`
	Revision        int64        `json:"revision"`
	Persona         string       `json:"persona"`
	Interests       []string     `json:"interests"`
	ServerTime      time.Time    `json:"serverTime"`
	Timezone        string       `json:"timezone"`
	CurrentTopic    *HostTopic   `json:"currentTopic"`
	SpokenTurns     []SpokenTurn `json:"spokenTurns"`
	ViewerTurns     []ViewerTurn `json:"viewerTurns"`
	RecentTopicKeys []string     `json:"recentTopicKeys"`
}
type SpeechCandidate struct {
	Source  string    `json:"source"`
	Text    string    `json:"text"`
	Context HostInput `json:"context"`
}
type SafetyDecision struct {
	Allow    bool   `json:"allow"`
	Category string `json:"category"`
}
type HostStatus struct {
	Mode           string     `json:"mode"`
	State          string     `json:"state"`
	TopicTitle     string     `json:"topicTitle"`
	LastError      string     `json:"lastError"`
	NextEligibleAt *time.Time `json:"nextEligibleAt"`
}
