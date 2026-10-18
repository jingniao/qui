package access

import "errors"

const MaxBatchRecords = 256

type Batch struct {
	ID      string   `json:"id"`
	NodeID  string   `json:"node_id"`
	Records []Rollup `json:"records"`
}

func (b *Batch) Validate() error {
	if !identifier(b.ID, true) || !identifier(b.NodeID, true) || len(b.Records) < 1 || len(b.Records) > MaxBatchRecords {
		return errors.New("invalid access batch")
	}
	for index := range b.Records {
		if b.Records[index].NodeID != b.NodeID {
			return errors.New("access batch node mismatch")
		}
		if err := b.Records[index].Normalize(); err != nil {
			return err
		}
	}
	return nil
}
