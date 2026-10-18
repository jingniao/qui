package access

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

const MaxBatchBytes = 1 << 20

func DecodeBatch(data []byte) (Batch, error) {
	var batch Batch
	if len(data) == 0 || len(data) > MaxBatchBytes {
		return batch, errors.New("access batch size invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&batch); err != nil {
		return batch, errors.New("invalid access batch JSON")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return batch, errors.New("access batch must contain one JSON object")
	}
	return batch, batch.Validate()
}
