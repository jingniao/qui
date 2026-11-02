package core

import "proxycontrol/pkg/contracts/desiredstate"

// KnownName reports whether a name is one of the core kinds this Agent runs. The
// configuration loader, the inventory, the core-run markers, the component holds
// and the membership checks share it, so another core kind is admitted in one
// place instead of in every list.
func KnownName(name string) bool { return desiredstate.KnownCore(name) }

// CoreNames is every core kind this Agent runs, in the order the component
// workers enumerate them.
func CoreNames() []string {
	return []string{desiredstate.CoreXray, desiredstate.CoreSingBox, desiredstate.CoreGost}
}
