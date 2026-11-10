package release

// OfficialKeyPins are the release keys of the official qui builds. Revoked pins stay listed.
var OfficialKeyPins = []KeyPin{
	{
		PublicKey:  "HIFPXy0aPRHUmS16VXWwf2dnbCIYnf0xwZTxW49ZyJ4=",
		Components: []string{"proxy-agent"},
	},
}

// EffectiveKeyPins returns configured when it is non-empty, otherwise OfficialKeyPins.
func EffectiveKeyPins(configured []KeyPin) []KeyPin {
	if len(configured) > 0 {
		return configured
	}
	return OfficialKeyPins
}
