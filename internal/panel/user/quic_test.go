package user

import (
	"bytes"
	"context"
	"encoding/json"
	"proxycontrol/pkg/contracts/desiredstate"
	"testing"
)

func TestQUICDeviceCredentialsIndependentEncryptedAndRotatable(t *testing.T) {
	ctx := context.Background()
	repository := testRepository(t)
	input := userInput()
	input.AllowedProtocols = []string{"hysteria2", "tuic"}
	u, err := repository.Save(ctx, "", input)
	if err != nil {
		t.Fatal(err)
	}
	first, err := repository.CreateDevice(ctx, u.ID, DeviceInput{Name: "First", Protocols: input.AllowedProtocols})
	if err != nil {
		t.Fatal(err)
	}
	second, err := repository.CreateDevice(ctx, u.ID, DeviceInput{Name: "Second", Protocols: input.AllowedProtocols})
	if err != nil {
		t.Fatal(err)
	}
	for index, credential := range first.Credentials {
		if credential.Secret == second.Credentials[index].Secret {
			t.Fatal("QUIC device passwords reused")
		}
		if credential.Protocol == "tuic" {
			if err := desiredstate.ValidateUUID(credential.Username); err != nil {
				t.Fatal(err)
			}
			if credential.Username == second.Credentials[index].Username {
				t.Fatal("TUIC UUID reused")
			}
		}
		var sealed []byte
		if err := repository.Database.QueryRow("SELECT encrypted_secret FROM credentials WHERE id=?", credential.ID).Scan(&sealed); err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(sealed, []byte(credential.Secret)) {
			t.Fatal("QUIC password persisted without encryption")
		}
	}
	hidden, err := repository.Device(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(hidden)
	for _, credential := range first.Credentials {
		if bytes.Contains(data, []byte(credential.Secret)) {
			t.Fatal("normal device query reveals QUIC secret")
		}
	}
	rotated, err := repository.RotateCredential(ctx, first.ID, "tuic", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := desiredstate.ValidateUUID(rotated.Username); err != nil {
		t.Fatal(err)
	}
}
