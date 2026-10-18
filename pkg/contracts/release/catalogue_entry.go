package release

// CatalogueEntry is shared metadata, with no signing private key or executable
// path. Modules expose it through public read interfaces.
type CatalogueEntry struct {
	ID        string `json:"id"`
	Signed    Signed `json:"signed"`
	CreatedBy string `json:"createdBy"`
	CreatedAt string `json:"createdAt"`
}
