package desiredstate

import "time"

// RuntimeAt derives the currently valid accounts without mutating the approved
// snapshot. Removing every account never turns a previously authenticated
// listener into a no-auth listener.
func (s State) RuntimeAt(at time.Time) (State, error) {
	result := s
	result.Users = append([]User(nil), s.Users...)
	inactive := map[string]bool{}
	for index, user := range result.Users {
		if !user.Enabled {
			inactive[user.ID] = true
		}
		if user.ExpiresAt != "" {
			expires, err := time.Parse(time.RFC3339Nano, user.ExpiresAt)
			if err != nil {
				return State{}, err
			}
			if !at.Before(expires) {
				result.Users[index].Enabled = false
				inactive[user.ID] = true
			}
		}
	}
	return result.filterInactive(inactive)
}

// WithoutCredentials derives a state without the given credentials, which is the
// same removal RuntimeAt applies to an inactive user's credentials: each one
// disappears from the credential list, from the inbound bindings and from the
// WireGuard peers, and an inbound left without credentials is disabled. An
// inbound therefore never becomes a listener without authentication, and the
// receiver keeps its own values.
func (s State) WithoutCredentials(remove map[string]bool) (State, error) {
	result := s
	result.Credentials = []Credential{}
	for _, credential := range s.Credentials {
		if remove[credential.ID] {
			continue
		}
		result.Credentials = append(result.Credentials, credential)
	}
	result.Inbounds = append([]Inbound(nil), s.Inbounds...)
	for index, inbound := range s.Inbounds {
		bound := []string{}
		for _, id := range inbound.CredentialIDs {
			if !remove[id] {
				bound = append(bound, id)
			}
		}
		result.Inbounds[index].CredentialIDs = bound
		if len(inbound.CredentialIDs) > 0 && len(bound) == 0 {
			result.Inbounds[index].Enabled = false
		}
		if inbound.Settings.WireGuard != nil {
			settings := *inbound.Settings.WireGuard
			settings.Addresses = append([]string(nil), settings.Addresses...)
			settings.Peers = []WireGuardPeer{}
			for _, peer := range inbound.Settings.WireGuard.Peers {
				if remove[peer.CredentialID] {
					continue
				}
				copy := peer
				copy.AllowedIPs = append([]string(nil), peer.AllowedIPs...)
				settings.Peers = append(settings.Peers, copy)
			}
			result.Inbounds[index].Settings.WireGuard = &settings
		}
	}
	return result, nil
}

// filterInactive removes the credentials of the given inactive users through the
// same path as any other credential removal.
func (s State) filterInactive(inactive map[string]bool) (State, error) {
	removed := map[string]bool{}
	for _, credential := range s.Credentials {
		if !credential.Internal && credential.UserID != "" && inactive[credential.UserID] {
			removed[credential.ID] = true
		}
	}
	return s.WithoutCredentials(removed)
}
