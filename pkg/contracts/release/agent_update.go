package release

// AgentUpdateRequest is deliberately distinct from core commands. Old core
// decoders continue to reject proxy-agent rather than executing it as a core.
type AgentUpdateRequest UpdateRequest

func (r AgentUpdateRequest) Validate() error {
	return validateUpdateRequest(UpdateRequest(r), r.Release.Manifest.Component == "proxy-agent")
}

func DecodeAgentUpdateRequest(data []byte) (AgentUpdateRequest, error) {
	var request AgentUpdateRequest
	if strictUpdate(data, MaxUpdateRequestBytes, &request) != nil {
		return request, ErrUpdate
	}
	return request, request.Validate()
}

func ValidateAgentResult(request AgentUpdateRequest, result UpdateResult) error {
	if request.Validate() != nil || result.Validate() != nil || result.ID != request.ID || result.NodeID != request.NodeID || result.Revision != request.Revision || result.TargetDigest != request.Release.Manifest.SHA256 {
		return ErrUpdate
	}
	if result.Status == "success" && (result.InstalledVersion != request.Release.Manifest.Version || result.InstalledSequence != request.Release.Manifest.Sequence) {
		return ErrUpdate
	}
	if request.Operation == "recover" && result.Status == "rejected" {
		return ErrUpdate
	}
	return nil
}
