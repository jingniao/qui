package core

// OSEnvironment is the fixed, local OS facts that bound the observation
// environment of a recorded child core. A fresh parent compares the recorded
// environment with its own before it may treat a process identity as proof of
// exit: a process recorded on another host, another boot or another PID namespace
// cannot be resolved by probing a PID number on this machine.
//
// Empty fields mean the platform could not supply that fact. An absent fact is
// never read as a proven exit.
type OSEnvironment struct {
	HostID      string `json:"host_id,omitempty"`
	BootID      string `json:"boot_id,omitempty"`
	NamespaceID string `json:"namespace_id,omitempty"`
}

// ObserveOSEnvironment reads only fixed local OS facts (files or links the
// kernel owns). It never runs a command, never signals a process and never
// shells out.
func ObserveOSEnvironment() OSEnvironment { return observeOSEnvironment() }
