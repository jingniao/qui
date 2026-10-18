// Package credential defines the persisted typed credential payload shared by
// User storage and read-only backup validation. It is never an API response.
package credential

type Secret struct {
	Value     string `json:"value"`
	PublicKey string `json:"public_key,omitempty"`
	Username  string `json:"username,omitempty"`
}
