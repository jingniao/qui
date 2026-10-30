package proxy

import (
	"context"
	"errors"
	"fmt"
	"math"
	"proxycontrol/pkg/contracts/desiredstate"
	"strings"
)

// A canvas scheme is a drawing, not a deployment: it stores the references and
// the layout, and publishing it later (U2c) translates those references into
// the existing outbounds, outbound groups and proxy chains through this module.
// Saving or deleting a scheme therefore never touches a proxy object.
const (
	canvasExecutionServer = "server"
	canvasExecutionClient = "client"

	canvasMaxInstances   = 50
	canvasMaxConnections = 100
	canvasMaxNameBytes   = 80
	canvasMaxTokenBytes  = 128
	canvasMaxLayoutBytes = 1 << 20
)

// ErrStaleScheme reports an update built on a version the caller no longer
// holds, because another administrator saved the same scheme first.
var ErrStaleScheme = errors.New("canvas scheme was modified elsewhere")

// ErrDuplicateSchemeName reports a second scheme with a name that is already in
// use. Publishing derives every generated proxy object's name from the scheme
// name, so two drawings with the same name would prune each other's objects.
var ErrDuplicateSchemeName = errors.New("canvas scheme name already in use")

// CanvasLayout is the typed body of a scheme. Unknown fields are refused when
// the request is decoded and when the stored JSON is read back, so what the
// server keeps always matches these structs.
type CanvasLayout struct {
	Instances   []CanvasInstance   `json:"instances"`
	Connections []CanvasConnection `json:"connections"`
}

// CanvasInstance is one box on the canvas. Ref is server:<nodeId>,
// inbound:<inboundId> or import:<opaque id>. The display fields the canvas
// shows (name, protocol, address, status) are derived from that reference when
// it loads, so they are deliberately not stored here.
type CanvasInstance struct {
	InstanceID string  `json:"instanceId"`
	Ref        string  `json:"ref"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Role       string  `json:"role"`
	ListenPort *int    `json:"listenPort,omitempty"`
	TargetPort *int    `json:"targetPort,omitempty"`
}

// CanvasConnection is one directed arrow between two instances. TunnelProtocol
// is accepted for every protocol the canvas can name; publishing (U2c) refuses
// the ones the official cores cannot carry.
type CanvasConnection struct {
	TunnelID       string `json:"tunnelId,omitempty"`
	ID             string `json:"id"`
	Source         string `json:"source"`
	Target         string `json:"target"`
	Type           string `json:"type"`
	TunnelProtocol string `json:"tunnelProtocol,omitempty"`
	TunnelPort     *int   `json:"tunnelPort,omitempty"`
	SupportTCP     bool   `json:"supportTcp"`
	SupportUDP     bool   `json:"supportUdp"`
	ListenPort     *int   `json:"listenPort,omitempty"`
	TargetHost     string `json:"targetHost,omitempty"`
	TargetPort     *int   `json:"targetPort,omitempty"`
}

var (
	canvasExecutions      = map[string]bool{canvasExecutionServer: true, canvasExecutionClient: true}
	canvasRoles           = map[string]bool{"entry": true, "relay": true, "exit": true}
	canvasConnectionKinds = map[string]bool{"proxy_chain": true, "tunnel": true, "port_forward": true}
	// Gost and Brook are third-party binaries outside D1 and AGENTS section 23,
	// and nftables forwarding would need a new privileged operation, so U2c will
	// refuse them. Storage accepts them so a drawing is never lost.
	canvasTunnelProtocols = map[string]bool{"tls": true, "wireguard": true, "gost": true, "brook": true, "nftables": true}
)

// canvasRef is one parsed instance reference.
type canvasRef struct{ kind, id string }

func parseCanvasRef(value string) (canvasRef, error) {
	kind, target, found := strings.Cut(value, ":")
	if !found {
		return canvasRef{}, errors.New("canvas instance ref must be server:<id>, inbound:<id> or import:<id>")
	}
	switch kind {
	case "server", "inbound", "import":
	default:
		return canvasRef{}, fmt.Errorf("unsupported canvas instance ref %q", kind)
	}
	if !validCanvasToken(target) {
		return canvasRef{}, fmt.Errorf("invalid %s reference id", kind)
	}
	return canvasRef{kind: kind, id: target}, nil
}

// validCanvasToken accepts the ids the canvas generates and the ids the panel
// generates (base64url node and inbound ids), which share this alphabet.
func validCanvasToken(value string) bool {
	if value == "" || len(value) > canvasMaxTokenBytes {
		return false
	}
	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z', char >= '0' && char <= '9':
		case char == '-', char == '_', char == '.':
		default:
			return false
		}
	}
	return true
}

func canvasPort(label string, port *int) error {
	if port == nil {
		return nil
	}
	if *port < 1 || *port > 65535 {
		return fmt.Errorf("canvas %s must be between 1 and 65535", label)
	}
	return nil
}

// validate refuses a layout that is not a well-formed directed graph, so the
// canvas cannot store something its own editor could never draw.
func (layout CanvasLayout) validate() error {
	if len(layout.Instances) > canvasMaxInstances {
		return fmt.Errorf("a canvas scheme holds at most %d instances", canvasMaxInstances)
	}
	if len(layout.Connections) > canvasMaxConnections {
		return fmt.Errorf("a canvas scheme holds at most %d connections", canvasMaxConnections)
	}
	instances := make(map[string]bool, len(layout.Instances))
	for _, instance := range layout.Instances {
		if !validCanvasToken(instance.InstanceID) {
			return errors.New("invalid canvas instance id")
		}
		if instances[instance.InstanceID] {
			return fmt.Errorf("duplicate canvas instance id %q", instance.InstanceID)
		}
		instances[instance.InstanceID] = true
		if _, err := parseCanvasRef(instance.Ref); err != nil {
			return err
		}
		if !canvasRoles[instance.Role] {
			return errors.New("invalid canvas instance role")
		}
		if math.IsNaN(instance.X) || math.IsInf(instance.X, 0) || math.IsNaN(instance.Y) || math.IsInf(instance.Y, 0) {
			return errors.New("invalid canvas instance coordinates")
		}
		if err := canvasPort("listen port", instance.ListenPort); err != nil {
			return err
		}
		if err := canvasPort("target port", instance.TargetPort); err != nil {
			return err
		}
	}
	connections := make(map[string]bool, len(layout.Connections))
	edges := make(map[string]bool, len(layout.Connections))
	for _, connection := range layout.Connections {
		if !validCanvasToken(connection.ID) {
			return errors.New("invalid canvas connection id")
		}
		if connections[connection.ID] {
			return fmt.Errorf("duplicate canvas connection id %q", connection.ID)
		}
		connections[connection.ID] = true
		// Connection-level forwarding settings are never silently discarded.
		if connection.Type != "port_forward" && (connection.ListenPort != nil || connection.TargetPort != nil || connection.TargetHost != "") {
			return fmt.Errorf("connection %q: forwarding settings require port_forward", connection.ID)
		}
		if err := canvasPort("listen port", connection.ListenPort); err != nil {
			return fmt.Errorf("connection %q: %w", connection.ID, err)
		}
		if err := canvasPort("target port", connection.TargetPort); err != nil {
			return fmt.Errorf("connection %q: %w", connection.ID, err)
		}
		if connection.TargetHost != "" {
			settings := desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{Host: connection.TargetHost, Port: 1, Network: "tcp"}}
			if err := desiredstate.ValidateSettings("forward", settings, true); err != nil {
				return fmt.Errorf("connection %q: %w", connection.ID, err)
			}
		}
		if !canvasConnectionKinds[connection.Type] {
			return errors.New("invalid canvas connection type")
		}
		if connection.TunnelProtocol != "" && !canvasTunnelProtocols[connection.TunnelProtocol] {
			return errors.New("invalid canvas tunnel protocol")
		}
		if !instances[connection.Source] || !instances[connection.Target] {
			return errors.New("canvas connection references an instance the scheme does not hold")
		}
		if connection.Source == connection.Target {
			return errors.New("canvas connection cannot start and end on the same instance")
		}
		edge := connection.Source + " -> " + connection.Target
		if connection.Type == "port_forward" {
			edge += ":forward:" + connection.ID
		}
		if edges[edge] {
			return fmt.Errorf("duplicate canvas connection %q", edge)
		}
		edges[edge] = true
		if err := canvasPort("tunnel port", connection.TunnelPort); err != nil {
			return err
		}
	}
	return nil
}

// validateRefs refuses a drawing that points at a node or inbound the panel no
// longer has. Unresolved import references may stay in drafts; client
// publication requires each imported ID to exist in the panel cache.
func (s *Service) validateRefs(ctx context.Context, layout CanvasLayout) error {
	for _, instance := range layout.Instances {
		ref, err := parseCanvasRef(instance.Ref)
		if err != nil {
			return err
		}
		var present bool
		switch ref.kind {
		case "server":
			present, err = s.Repository.canvasNodePresent(ctx, ref.id)
		case "inbound":
			present, err = s.Repository.canvasInboundPresent(ctx, ref.id)
		default:
			continue
		}
		if err != nil {
			return err
		}
		if !present {
			return fmt.Errorf("canvas instance %q references a missing or deleted %s", instance.InstanceID, ref.kind)
		}
	}
	return nil
}

// SaveCanvasScheme validates a drawing and stores it. An empty schemeID creates
// a scheme and returns it with the id the panel generated; a set schemeID
// updates it only when input.UpdatedAt still matches the stored version.
func (s *Service) SaveCanvasScheme(ctx context.Context, schemeID string, input CanvasSchemeInput) (CanvasScheme, error) {
	s.canvasMu.Lock()
	defer s.canvasMu.Unlock()
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > canvasMaxNameBytes {
		return CanvasScheme{}, fmt.Errorf("canvas scheme name must be 1 to %d bytes", canvasMaxNameBytes)
	}
	// Generated objects are named "[<scheme>] …"; a bracket inside the name
	// would let one scheme's prefix match another scheme's objects.
	if strings.ContainsAny(name, "[]") {
		return CanvasScheme{}, errors.New("方案名称不能包含方括号 [ ]")
	}
	if !canvasExecutions[input.Execution] {
		return CanvasScheme{}, errors.New("invalid canvas scheme execution")
	}
	if err := s.Repository.ReconcileCanvasTunnels(ctx); err != nil {
		return CanvasScheme{}, err
	}
	var resolveErr error
	input.Layout, resolveErr = s.Repository.resolveCanvasLayout(ctx, input.Execution, input.Layout)
	if resolveErr != nil {
		return CanvasScheme{}, resolveErr
	}
	if err := input.Layout.validate(); err != nil {
		return CanvasScheme{}, err
	}
	if err := s.validateRefs(ctx, input.Layout); err != nil {
		return CanvasScheme{}, err
	}
	if taken, err := s.Repository.canvasSchemeNameTaken(ctx, name, schemeID); err != nil {
		return CanvasScheme{}, err
	} else if taken {
		return CanvasScheme{}, ErrDuplicateSchemeName
	}
	input.Name = name
	return s.Repository.SaveCanvasScheme(ctx, schemeID, input)
}
