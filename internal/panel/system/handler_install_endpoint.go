package system

import (
	"net/http"

	"proxycontrol/internal/platform/httpapi"
)

// installEndpoint returns the saved panel address one-click Agent installation
// defaults to. Only the Owner sees the install dialogs, so the read is Owner-only
// as well. An unset value reads as host "" and httpPort 0.
func (h *Handler) installEndpoint(w http.ResponseWriter, r *http.Request) {
	value, err := h.Repository.ReadInstallEndpoint(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, value)
}

func (h *Handler) saveInstallEndpoint(w http.ResponseWriter, r *http.Request) {
	if h.ActorName == nil {
		httpapi.Error(w, 500, "internal_error", "Settings audit identity unavailable")
		return
	}
	var input InstallEndpointInput
	if err := httpapi.Decode(w, r, &input); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	value, _, err := h.Repository.SaveInstallEndpoint(r.Context(), input, h.ActorName(r.Context()), r.RemoteAddr)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, value)
}

// adminEntry returns the admin login path. Only the logged-in Owner may read it:
// no public endpoint discloses the address.
func (h *Handler) adminEntry(w http.ResponseWriter, r *http.Request) {
	value, err := h.Repository.ReadAdminEntry(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, value)
}

func (h *Handler) saveAdminEntry(w http.ResponseWriter, r *http.Request) {
	if h.ActorName == nil {
		httpapi.Error(w, 500, "internal_error", "Settings audit identity unavailable")
		return
	}
	var input AdminEntryInput
	if err := httpapi.Decode(w, r, &input); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	value, err := h.Repository.SaveAdminEntry(r.Context(), input, h.ActorName(r.Context()), r.RemoteAddr)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, value)
}
